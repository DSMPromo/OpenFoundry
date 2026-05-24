package dynamodb

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsddb "github.com/aws/aws-sdk-go-v2/service/dynamodb"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

type fakeDDBClient struct {
	describeErr  error
	scanInput    *awsddb.ScanInput
	scanResp     *awsddb.ScanOutput
	scanErr      error
	getItemInput *awsddb.GetItemInput
	getItemResp  *awsddb.GetItemOutput
	putItemInput *awsddb.PutItemInput
}

func (f *fakeDDBClient) DescribeTable(_ context.Context, _ *awsddb.DescribeTableInput, _ ...func(*awsddb.Options)) (*awsddb.DescribeTableOutput, error) {
	if f.describeErr != nil {
		return nil, f.describeErr
	}
	return &awsddb.DescribeTableOutput{}, nil
}

func (f *fakeDDBClient) Scan(_ context.Context, in *awsddb.ScanInput, _ ...func(*awsddb.Options)) (*awsddb.ScanOutput, error) {
	f.scanInput = in
	if f.scanErr != nil {
		return nil, f.scanErr
	}
	if f.scanResp != nil {
		return f.scanResp, nil
	}
	return &awsddb.ScanOutput{}, nil
}

func (f *fakeDDBClient) GetItem(_ context.Context, in *awsddb.GetItemInput, _ ...func(*awsddb.Options)) (*awsddb.GetItemOutput, error) {
	f.getItemInput = in
	if f.getItemResp != nil {
		return f.getItemResp, nil
	}
	return &awsddb.GetItemOutput{}, nil
}

func (f *fakeDDBClient) PutItem(_ context.Context, in *awsddb.PutItemInput, _ ...func(*awsddb.Options)) (*awsddb.PutItemOutput, error) {
	f.putItemInput = in
	return &awsddb.PutItemOutput{}, nil
}

func TestConfigFromJSONRequiresTableName(t *testing.T) {
	if _, err := ConfigFromJSON(nil); err == nil {
		t.Fatal("want error on empty config")
	}
	if _, err := ConfigFromJSON(json.RawMessage(`{}`)); err == nil {
		t.Fatal("want error when table_name missing")
	}
	cfg, err := ConfigFromJSON(json.RawMessage(`{"table_name":"kv"}`))
	if err != nil || cfg.TableName != "kv" {
		t.Fatalf("parse wrong: %+v err=%v", cfg, err)
	}
}

func TestConnectWrapsSDKError(t *testing.T) {
	client := &fakeDDBClient{describeErr: errors.New("ResourceNotFoundException")}
	d := NewWithClient(Config{TableName: "missing"}, client)
	err := d.Connect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "ResourceNotFoundException") {
		t.Fatalf("err = %v", err)
	}
}

func TestScanProjectsItemsAndClampsLimit(t *testing.T) {
	client := &fakeDDBClient{scanResp: &awsddb.ScanOutput{
		Items: []map[string]ddbtypes.AttributeValue{
			{
				"id":   &ddbtypes.AttributeValueMemberS{Value: "a"},
				"hits": &ddbtypes.AttributeValueMemberN{Value: "12"},
				"on":   &ddbtypes.AttributeValueMemberBOOL{Value: true},
			},
		},
	}}
	d := NewWithClient(Config{TableName: "kv"}, client)
	rows, err := d.Scan(context.Background(), 5000) // > 1000, must clamp
	if err != nil {
		t.Fatal(err)
	}
	if got := aws.ToInt32(client.scanInput.Limit); got != 1000 {
		t.Errorf("limit not clamped: %d", got)
	}
	if len(rows) != 1 || rows[0]["id"] != "a" {
		t.Fatalf("projection wrong: %+v", rows)
	}
	// attributevalue.UnmarshalMap decodes DynamoDB N into a Go
	// numeric type (float64 via map[string]any) — the JSON marshaler
	// then re-encodes it numerically. Assert both shapes are
	// preserved sensibly.
	switch v := rows[0]["hits"].(type) {
	case float64:
		if v != 12 {
			t.Errorf("number projection: %v", v)
		}
	default:
		t.Errorf("number projection unexpected type %T: %v", v, v)
	}
	if rows[0]["on"] != true {
		t.Errorf("bool projection: %v", rows[0]["on"])
	}
}

func TestGetItemReturnsNilWhenMissing(t *testing.T) {
	client := &fakeDDBClient{getItemResp: &awsddb.GetItemOutput{Item: nil}}
	d := NewWithClient(Config{TableName: "kv"}, client)
	row, err := d.GetItem(context.Background(), "id", "x")
	if err != nil {
		t.Fatal(err)
	}
	if row != nil {
		t.Errorf("want nil row, got %+v", row)
	}
}

func TestPutItemMarshalsMap(t *testing.T) {
	client := &fakeDDBClient{}
	d := NewWithClient(Config{TableName: "kv"}, client)
	if err := d.PutItem(context.Background(), map[string]any{"id": "x", "n": 42}); err != nil {
		t.Fatal(err)
	}
	if v, ok := client.putItemInput.Item["id"].(*ddbtypes.AttributeValueMemberS); !ok || v.Value != "x" {
		t.Errorf("id encoding wrong: %+v", client.putItemInput.Item["id"])
	}
}
