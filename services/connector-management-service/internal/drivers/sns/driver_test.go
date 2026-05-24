package sns

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssns "github.com/aws/aws-sdk-go-v2/service/sns"
	snstypes "github.com/aws/aws-sdk-go-v2/service/sns/types"
)

type fakeSNSClient struct {
	getAttrsErr  error
	publishInput *awssns.PublishInput
	publishResp  *awssns.PublishOutput
	publishErr   error
	listResp     *awssns.ListTopicsOutput
}

func (f *fakeSNSClient) GetTopicAttributes(_ context.Context, _ *awssns.GetTopicAttributesInput, _ ...func(*awssns.Options)) (*awssns.GetTopicAttributesOutput, error) {
	if f.getAttrsErr != nil {
		return nil, f.getAttrsErr
	}
	return &awssns.GetTopicAttributesOutput{}, nil
}

func (f *fakeSNSClient) Publish(_ context.Context, in *awssns.PublishInput, _ ...func(*awssns.Options)) (*awssns.PublishOutput, error) {
	f.publishInput = in
	if f.publishErr != nil {
		return nil, f.publishErr
	}
	if f.publishResp != nil {
		return f.publishResp, nil
	}
	return &awssns.PublishOutput{MessageId: aws.String("pub-1")}, nil
}

func (f *fakeSNSClient) ListTopics(_ context.Context, _ *awssns.ListTopicsInput, _ ...func(*awssns.Options)) (*awssns.ListTopicsOutput, error) {
	if f.listResp != nil {
		return f.listResp, nil
	}
	return &awssns.ListTopicsOutput{}, nil
}

func TestConfigFromJSONRequiresTopicARN(t *testing.T) {
	if _, err := ConfigFromJSON(nil); err == nil {
		t.Fatal("want error on empty config")
	}
	if _, err := ConfigFromJSON(json.RawMessage(`{}`)); err == nil {
		t.Fatal("want error when topic_arn missing")
	}
	cfg, err := ConfigFromJSON(json.RawMessage(`{"topic_arn":"arn:aws:sns:us-east-1:0:t"}`))
	if err != nil || cfg.TopicARN == "" {
		t.Fatalf("topic_arn not parsed: %+v err=%v", cfg, err)
	}
}

func TestConnectWrapsSDKError(t *testing.T) {
	client := &fakeSNSClient{getAttrsErr: errors.New("NotFound")}
	d := NewWithClient(Config{TopicARN: "arn:aws:sns:us-east-1:0:t"}, client)
	err := d.Connect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "NotFound") {
		t.Fatalf("err = %v", err)
	}
}

func TestPublishRoutesMessageAndAttributes(t *testing.T) {
	client := &fakeSNSClient{}
	d := NewWithClient(Config{TopicARN: "arn:aws:sns:us-east-1:0:t"}, client)
	id, err := d.Publish(context.Background(), "hello", "greeting", map[string]string{"trace": "abc"})
	if err != nil {
		t.Fatal(err)
	}
	if id != "pub-1" {
		t.Errorf("message id = %q", id)
	}
	if aws.ToString(client.publishInput.Message) != "hello" {
		t.Errorf("message not routed")
	}
	if aws.ToString(client.publishInput.Subject) != "greeting" {
		t.Errorf("subject not routed")
	}
	if v := client.publishInput.MessageAttributes["trace"]; aws.ToString(v.StringValue) != "abc" {
		t.Errorf("attribute not routed: %+v", v)
	}
}

func TestPublishOmitsSubjectAndAttributesWhenEmpty(t *testing.T) {
	client := &fakeSNSClient{}
	d := NewWithClient(Config{TopicARN: "arn:aws:sns:us-east-1:0:t"}, client)
	if _, err := d.Publish(context.Background(), "x", "", nil); err != nil {
		t.Fatal(err)
	}
	if client.publishInput.Subject != nil {
		t.Errorf("subject should be nil when empty")
	}
	if client.publishInput.MessageAttributes != nil {
		t.Errorf("attributes should be nil when empty")
	}
}

func TestListProjectsTopics(t *testing.T) {
	client := &fakeSNSClient{listResp: &awssns.ListTopicsOutput{
		Topics: []snstypes.Topic{
			{TopicArn: aws.String("arn:1")},
			{TopicArn: aws.String("arn:2")},
		},
	}}
	d := NewWithClient(Config{TopicARN: "arn:any"}, client)
	got, err := d.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ARN != "arn:1" || got[1].ARN != "arn:2" {
		t.Fatalf("projection wrong: %+v", got)
	}
}
