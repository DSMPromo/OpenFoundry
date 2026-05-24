// Package sns is the runtime client for the AWS SNS connector. SNS
// is publish-only from this service's perspective (subscription
// fan-out lives downstream in notification-alerting-service or a
// future Lambda subscriber); the driver therefore exposes Connect /
// Publish / List.
package sns

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssns "github.com/aws/aws-sdk-go-v2/service/sns"
	snstypes "github.com/aws/aws-sdk-go-v2/service/sns/types"

	awsclient "github.com/openfoundry/openfoundry-go/libs/aws-client"
)

// Config carries SNS topic identification + AWS connection knobs.
type Config struct {
	// TopicARN is the canonical identifier — arn:aws:sns:<region>:
	// <account>:<topic>. Required.
	TopicARN string `json:"topic_arn"`

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
			return cfg, fmt.Errorf("sns: invalid config: %w", err)
		}
	}
	if strings.TrimSpace(cfg.TopicARN) == "" {
		return cfg, errors.New("sns: config requires 'topic_arn'")
	}
	return cfg, nil
}

// TopicSummary is the projected shape returned by List.
type TopicSummary struct {
	ARN string `json:"arn"`
}

// snsClient is the minimum surface the driver needs.
type snsClient interface {
	GetTopicAttributes(ctx context.Context, in *awssns.GetTopicAttributesInput, opts ...func(*awssns.Options)) (*awssns.GetTopicAttributesOutput, error)
	Publish(ctx context.Context, in *awssns.PublishInput, opts ...func(*awssns.Options)) (*awssns.PublishOutput, error)
	ListTopics(ctx context.Context, in *awssns.ListTopicsInput, opts ...func(*awssns.Options)) (*awssns.ListTopicsOutput, error)
}

// Driver wraps the SNS SDK client.
type Driver struct {
	cfg    Config
	client snsClient
}

// New builds a Driver from cfg.
func New(ctx context.Context, cfg Config) (*Driver, error) {
	client, err := awsclient.SNS(ctx, awsclient.Config{
		EndpointURL:     cfg.Endpoint,
		Region:          cfg.Region,
		AccessKeyID:     cfg.AccessKeyID,
		SecretAccessKey: cfg.SecretAccessKey,
		SessionToken:    cfg.SessionToken,
	})
	if err != nil {
		return nil, fmt.Errorf("sns: %w", err)
	}
	return &Driver{cfg: cfg, client: client}, nil
}

// NewWithClient is the test-friendly constructor.
func NewWithClient(cfg Config, client snsClient) *Driver {
	return &Driver{cfg: cfg, client: client}
}

// TopicARN reports the topic the driver is bound to.
func (d *Driver) TopicARN() string { return d.cfg.TopicARN }

// Connect verifies the topic exists via GetTopicAttributes.
func (d *Driver) Connect(ctx context.Context) error {
	if d == nil || d.client == nil {
		return errors.New("sns: driver is not initialized")
	}
	if _, err := d.client.GetTopicAttributes(ctx, &awssns.GetTopicAttributesInput{
		TopicArn: aws.String(d.cfg.TopicARN),
	}); err != nil {
		return fmt.Errorf("sns: GetTopicAttributes %s: %w", d.cfg.TopicARN, err)
	}
	return nil
}

// Publish sends a single message to the topic. Optional subject +
// string-typed attributes match the SNS Publish API surface.
func (d *Driver) Publish(ctx context.Context, message, subject string, attributes map[string]string) (string, error) {
	if d == nil || d.client == nil {
		return "", errors.New("sns: driver is not initialized")
	}
	in := &awssns.PublishInput{
		TopicArn: aws.String(d.cfg.TopicARN),
		Message:  aws.String(message),
	}
	if subject != "" {
		in.Subject = aws.String(subject)
	}
	if len(attributes) > 0 {
		in.MessageAttributes = make(map[string]snstypes.MessageAttributeValue, len(attributes))
		for k, v := range attributes {
			in.MessageAttributes[k] = snstypes.MessageAttributeValue{
				DataType:    aws.String("String"),
				StringValue: aws.String(v),
			}
		}
	}
	out, err := d.client.Publish(ctx, in)
	if err != nil {
		return "", fmt.Errorf("sns: Publish: %w", err)
	}
	return aws.ToString(out.MessageId), nil
}

// List returns topics visible to the configured credentials. SNS
// list is paginated; we collapse the first page for the connector
// picker UI — operators with hundreds of topics filter by name.
func (d *Driver) List(ctx context.Context) ([]TopicSummary, error) {
	if d == nil || d.client == nil {
		return nil, errors.New("sns: driver is not initialized")
	}
	out, err := d.client.ListTopics(ctx, &awssns.ListTopicsInput{})
	if err != nil {
		return nil, fmt.Errorf("sns: ListTopics: %w", err)
	}
	res := make([]TopicSummary, 0, len(out.Topics))
	for _, t := range out.Topics {
		res = append(res, TopicSummary{ARN: aws.ToString(t.TopicArn)})
	}
	return res, nil
}
