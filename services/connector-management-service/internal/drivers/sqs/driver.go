// Package sqs is the runtime client for the AWS SQS connector.
// Mirrors the shape of the s3 + lambda drivers: tiny Config, a
// [Driver] with Connect/Send/Receive/Delete on top of aws-sdk-go-v2.
package sqs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"

	awsclient "github.com/openfoundry/openfoundry-go/libs/aws-client"
)

// Config carries SQS queue identification + AWS connection knobs.
type Config struct {
	// QueueURL is the absolute SQS queue URL — preferred over
	// (QueueName + AccountID) because it works against LocalStack
	// without account discovery. Example:
	//   http://localhost:4566/000000000000/openfoundry-pipeline-events
	QueueURL string `json:"queue_url"`

	// QueueName + AccountID together work as a fallback when the
	// caller doesn't know the full URL; the driver resolves them via
	// GetQueueUrl on first use.
	QueueName string `json:"queue_name"`
	AccountID string `json:"account_id"`

	Endpoint        string `json:"endpoint"`
	Region          string `json:"region"`
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	SessionToken    string `json:"session_token"`
}

// ConfigFromJSON parses + validates a `connection.config` blob.
func ConfigFromJSON(raw json.RawMessage) (Config, error) {
	cfg := Config{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return cfg, fmt.Errorf("sqs: invalid config: %w", err)
		}
	}
	if strings.TrimSpace(cfg.QueueURL) == "" && strings.TrimSpace(cfg.QueueName) == "" {
		return cfg, errors.New("sqs: config requires 'queue_url' or 'queue_name'")
	}
	return cfg, nil
}

// Message is the projected shape of a single SQS message returned by
// Receive. ReceiptHandle is opaque — pass it back to Delete to ack
// the message.
type Message struct {
	MessageID     string            `json:"message_id"`
	ReceiptHandle string            `json:"receipt_handle"`
	Body          string            `json:"body"`
	Attributes    map[string]string `json:"attributes,omitempty"`
}

// sqsClient is the minimum surface the driver needs.
type sqsClient interface {
	GetQueueUrl(ctx context.Context, in *awssqs.GetQueueUrlInput, opts ...func(*awssqs.Options)) (*awssqs.GetQueueUrlOutput, error)
	GetQueueAttributes(ctx context.Context, in *awssqs.GetQueueAttributesInput, opts ...func(*awssqs.Options)) (*awssqs.GetQueueAttributesOutput, error)
	SendMessage(ctx context.Context, in *awssqs.SendMessageInput, opts ...func(*awssqs.Options)) (*awssqs.SendMessageOutput, error)
	ReceiveMessage(ctx context.Context, in *awssqs.ReceiveMessageInput, opts ...func(*awssqs.Options)) (*awssqs.ReceiveMessageOutput, error)
	DeleteMessage(ctx context.Context, in *awssqs.DeleteMessageInput, opts ...func(*awssqs.Options)) (*awssqs.DeleteMessageOutput, error)
}

// Driver is the SQS runtime client.
type Driver struct {
	cfg      Config
	client   sqsClient
	resolved string // cached queue URL
}

// New builds a Driver from cfg.
func New(ctx context.Context, cfg Config) (*Driver, error) {
	client, err := awsclient.SQS(ctx, awsclient.Config{
		EndpointURL:     cfg.Endpoint,
		Region:          cfg.Region,
		AccessKeyID:     cfg.AccessKeyID,
		SecretAccessKey: cfg.SecretAccessKey,
		SessionToken:    cfg.SessionToken,
	})
	if err != nil {
		return nil, fmt.Errorf("sqs: %w", err)
	}
	return &Driver{cfg: cfg, client: client, resolved: cfg.QueueURL}, nil
}

// NewWithClient is the test-friendly constructor.
func NewWithClient(cfg Config, client sqsClient) *Driver {
	return &Driver{cfg: cfg, client: client, resolved: cfg.QueueURL}
}

// QueueURL returns the cached / configured queue URL, resolving via
// GetQueueUrl the first time when only QueueName is set.
func (d *Driver) QueueURL(ctx context.Context) (string, error) {
	if d == nil || d.client == nil {
		return "", errors.New("sqs: driver is not initialized")
	}
	if d.resolved != "" {
		return d.resolved, nil
	}
	in := &awssqs.GetQueueUrlInput{QueueName: aws.String(d.cfg.QueueName)}
	if d.cfg.AccountID != "" {
		in.QueueOwnerAWSAccountId = aws.String(d.cfg.AccountID)
	}
	out, err := d.client.GetQueueUrl(ctx, in)
	if err != nil {
		return "", fmt.Errorf("sqs: GetQueueUrl %s: %w", d.cfg.QueueName, err)
	}
	d.resolved = aws.ToString(out.QueueUrl)
	return d.resolved, nil
}

// Connect verifies the queue is reachable via GetQueueAttributes
// (cheap; pulls the attribute list but no messages).
func (d *Driver) Connect(ctx context.Context) error {
	url, err := d.QueueURL(ctx)
	if err != nil {
		return err
	}
	_, err = d.client.GetQueueAttributes(ctx, &awssqs.GetQueueAttributesInput{
		QueueUrl:       aws.String(url),
		AttributeNames: []sqstypes.QueueAttributeName{sqstypes.QueueAttributeNameApproximateNumberOfMessages},
	})
	if err != nil {
		return fmt.Errorf("sqs: GetQueueAttributes %s: %w", url, err)
	}
	return nil
}

// Send publishes a single message to the queue.
func (d *Driver) Send(ctx context.Context, body string) (string, error) {
	url, err := d.QueueURL(ctx)
	if err != nil {
		return "", err
	}
	out, err := d.client.SendMessage(ctx, &awssqs.SendMessageInput{
		QueueUrl:    aws.String(url),
		MessageBody: aws.String(body),
	})
	if err != nil {
		return "", fmt.Errorf("sqs: SendMessage: %w", err)
	}
	return aws.ToString(out.MessageId), nil
}

// Receive polls the queue for up to `max` messages with the supplied
// long-poll wait. Returns the messages as the projected Message
// shape; ReceiptHandle is the token to pass to Delete.
func (d *Driver) Receive(ctx context.Context, max int32, waitSeconds int32) ([]Message, error) {
	if max <= 0 {
		max = 10
	}
	if max > 10 {
		max = 10 // SQS cap
	}
	url, err := d.QueueURL(ctx)
	if err != nil {
		return nil, err
	}
	out, err := d.client.ReceiveMessage(ctx, &awssqs.ReceiveMessageInput{
		QueueUrl:              aws.String(url),
		MaxNumberOfMessages:   max,
		WaitTimeSeconds:       waitSeconds,
		MessageAttributeNames: []string{"All"},
	})
	if err != nil {
		return nil, fmt.Errorf("sqs: ReceiveMessage: %w", err)
	}
	res := make([]Message, 0, len(out.Messages))
	for _, m := range out.Messages {
		msg := Message{
			MessageID:     aws.ToString(m.MessageId),
			ReceiptHandle: aws.ToString(m.ReceiptHandle),
			Body:          aws.ToString(m.Body),
		}
		if len(m.MessageAttributes) > 0 {
			msg.Attributes = make(map[string]string, len(m.MessageAttributes))
			for k, v := range m.MessageAttributes {
				msg.Attributes[k] = aws.ToString(v.StringValue)
			}
		}
		res = append(res, msg)
	}
	return res, nil
}

// Delete acks a previously-received message by its ReceiptHandle.
func (d *Driver) Delete(ctx context.Context, receiptHandle string) error {
	url, err := d.QueueURL(ctx)
	if err != nil {
		return err
	}
	if _, err := d.client.DeleteMessage(ctx, &awssqs.DeleteMessageInput{
		QueueUrl:      aws.String(url),
		ReceiptHandle: aws.String(receiptHandle),
	}); err != nil {
		return fmt.Errorf("sqs: DeleteMessage: %w", err)
	}
	return nil
}
