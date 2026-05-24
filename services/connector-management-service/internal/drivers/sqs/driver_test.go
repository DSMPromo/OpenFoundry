package sqs

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

type fakeSQSClient struct {
	getURLResp   *awssqs.GetQueueUrlOutput
	getURLErr    error
	getAttrsErr  error
	sendInput    *awssqs.SendMessageInput
	sendResp     *awssqs.SendMessageOutput
	sendErr      error
	receiveInput *awssqs.ReceiveMessageInput
	receiveResp  *awssqs.ReceiveMessageOutput
	receiveErr   error
	deleteInput  *awssqs.DeleteMessageInput
	deleteErr    error
}

func (f *fakeSQSClient) GetQueueUrl(_ context.Context, _ *awssqs.GetQueueUrlInput, _ ...func(*awssqs.Options)) (*awssqs.GetQueueUrlOutput, error) {
	if f.getURLErr != nil {
		return nil, f.getURLErr
	}
	if f.getURLResp != nil {
		return f.getURLResp, nil
	}
	return &awssqs.GetQueueUrlOutput{QueueUrl: aws.String("http://resolved/queue")}, nil
}

func (f *fakeSQSClient) GetQueueAttributes(_ context.Context, _ *awssqs.GetQueueAttributesInput, _ ...func(*awssqs.Options)) (*awssqs.GetQueueAttributesOutput, error) {
	if f.getAttrsErr != nil {
		return nil, f.getAttrsErr
	}
	return &awssqs.GetQueueAttributesOutput{}, nil
}

func (f *fakeSQSClient) SendMessage(_ context.Context, in *awssqs.SendMessageInput, _ ...func(*awssqs.Options)) (*awssqs.SendMessageOutput, error) {
	f.sendInput = in
	if f.sendErr != nil {
		return nil, f.sendErr
	}
	if f.sendResp != nil {
		return f.sendResp, nil
	}
	return &awssqs.SendMessageOutput{MessageId: aws.String("msg-1")}, nil
}

func (f *fakeSQSClient) ReceiveMessage(_ context.Context, in *awssqs.ReceiveMessageInput, _ ...func(*awssqs.Options)) (*awssqs.ReceiveMessageOutput, error) {
	f.receiveInput = in
	if f.receiveErr != nil {
		return nil, f.receiveErr
	}
	if f.receiveResp != nil {
		return f.receiveResp, nil
	}
	return &awssqs.ReceiveMessageOutput{}, nil
}

func (f *fakeSQSClient) DeleteMessage(_ context.Context, in *awssqs.DeleteMessageInput, _ ...func(*awssqs.Options)) (*awssqs.DeleteMessageOutput, error) {
	f.deleteInput = in
	return &awssqs.DeleteMessageOutput{}, f.deleteErr
}

func TestConfigFromJSONRequiresIdentity(t *testing.T) {
	if _, err := ConfigFromJSON(nil); err == nil {
		t.Fatal("want error on empty config")
	}
	if _, err := ConfigFromJSON(json.RawMessage(`{}`)); err == nil {
		t.Fatal("want error when both queue_url and queue_name missing")
	}
	cfg, err := ConfigFromJSON(json.RawMessage(`{"queue_url":"http://x/q"}`))
	if err != nil || cfg.QueueURL != "http://x/q" {
		t.Fatalf("queue_url not parsed: %+v err=%v", cfg, err)
	}
}

func TestQueueURLResolvesByName(t *testing.T) {
	client := &fakeSQSClient{getURLResp: &awssqs.GetQueueUrlOutput{QueueUrl: aws.String("http://resolved/q")}}
	d := NewWithClient(Config{QueueName: "demo"}, client)
	url, err := d.QueueURL(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if url != "http://resolved/q" {
		t.Fatalf("url = %q", url)
	}
	// Cached after first call.
	url2, _ := d.QueueURL(context.Background())
	if url2 != url {
		t.Errorf("cache miss: %q vs %q", url, url2)
	}
}

func TestConnectCallsGetQueueAttributes(t *testing.T) {
	client := &fakeSQSClient{}
	d := NewWithClient(Config{QueueURL: "http://x/q"}, client)
	if err := d.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestConnectWrapsSDKError(t *testing.T) {
	client := &fakeSQSClient{getAttrsErr: errors.New("AccessDeniedException")}
	d := NewWithClient(Config{QueueURL: "http://x/q"}, client)
	err := d.Connect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "AccessDeniedException") {
		t.Fatalf("err = %v", err)
	}
}

func TestSendRoutesBody(t *testing.T) {
	client := &fakeSQSClient{}
	d := NewWithClient(Config{QueueURL: "http://x/q"}, client)
	id, err := d.Send(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if id != "msg-1" {
		t.Errorf("message id = %q", id)
	}
	if aws.ToString(client.sendInput.MessageBody) != "hello" {
		t.Errorf("body not routed: %q", aws.ToString(client.sendInput.MessageBody))
	}
}

func TestReceiveClampsMaxAndProjectsMessages(t *testing.T) {
	client := &fakeSQSClient{
		receiveResp: &awssqs.ReceiveMessageOutput{
			Messages: []sqstypes.Message{
				{
					MessageId:     aws.String("m1"),
					ReceiptHandle: aws.String("h1"),
					Body:          aws.String("hi"),
					MessageAttributes: map[string]sqstypes.MessageAttributeValue{
						"trace": {StringValue: aws.String("abc")},
					},
				},
			},
		},
	}
	d := NewWithClient(Config{QueueURL: "http://x/q"}, client)
	msgs, err := d.Receive(context.Background(), 50, 0) // 50 > 10, must clamp
	if err != nil {
		t.Fatal(err)
	}
	if client.receiveInput.MaxNumberOfMessages != 10 {
		t.Errorf("max not clamped: %d", client.receiveInput.MaxNumberOfMessages)
	}
	if len(msgs) != 1 || msgs[0].Body != "hi" || msgs[0].Attributes["trace"] != "abc" {
		t.Errorf("projection wrong: %+v", msgs)
	}
}

func TestDelete(t *testing.T) {
	client := &fakeSQSClient{}
	d := NewWithClient(Config{QueueURL: "http://x/q"}, client)
	if err := d.Delete(context.Background(), "h1"); err != nil {
		t.Fatal(err)
	}
	if aws.ToString(client.deleteInput.ReceiptHandle) != "h1" {
		t.Errorf("handle not routed: %q", aws.ToString(client.deleteInput.ReceiptHandle))
	}
}
