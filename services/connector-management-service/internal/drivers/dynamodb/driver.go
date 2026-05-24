// Package dynamodb is the runtime client for the DynamoDB connector.
// Operators register a table as a connection; the driver exposes
// Connect (DescribeTable), Scan, GetItem, and PutItem on top of
// aws-sdk-go-v2.
package dynamodb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	awsddb "github.com/aws/aws-sdk-go-v2/service/dynamodb"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	awsclient "github.com/openfoundry/openfoundry-go/libs/aws-client"
)

// Config carries DynamoDB table identification + AWS connection
// knobs.
type Config struct {
	TableName       string `json:"table_name"`
	Endpoint        string `json:"endpoint"`
	Region          string `json:"region"`
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	SessionToken    string `json:"session_token"`
}

func ConfigFromJSON(raw json.RawMessage) (Config, error) {
	cfg := Config{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return cfg, fmt.Errorf("dynamodb: invalid config: %w", err)
		}
	}
	if strings.TrimSpace(cfg.TableName) == "" {
		return cfg, errors.New("dynamodb: config requires 'table_name'")
	}
	return cfg, nil
}

// ddbClient is the minimum surface the driver needs.
type ddbClient interface {
	DescribeTable(ctx context.Context, in *awsddb.DescribeTableInput, opts ...func(*awsddb.Options)) (*awsddb.DescribeTableOutput, error)
	Scan(ctx context.Context, in *awsddb.ScanInput, opts ...func(*awsddb.Options)) (*awsddb.ScanOutput, error)
	GetItem(ctx context.Context, in *awsddb.GetItemInput, opts ...func(*awsddb.Options)) (*awsddb.GetItemOutput, error)
	PutItem(ctx context.Context, in *awsddb.PutItemInput, opts ...func(*awsddb.Options)) (*awsddb.PutItemOutput, error)
}

type Driver struct {
	cfg    Config
	client ddbClient
}

func New(ctx context.Context, cfg Config) (*Driver, error) {
	client, err := awsclient.DynamoDB(ctx, awsclient.Config{
		EndpointURL:     cfg.Endpoint,
		Region:          cfg.Region,
		AccessKeyID:     cfg.AccessKeyID,
		SecretAccessKey: cfg.SecretAccessKey,
		SessionToken:    cfg.SessionToken,
	})
	if err != nil {
		return nil, fmt.Errorf("dynamodb: %w", err)
	}
	return &Driver{cfg: cfg, client: client}, nil
}

// NewWithClient is the test-friendly constructor.
func NewWithClient(cfg Config, client ddbClient) *Driver {
	return &Driver{cfg: cfg, client: client}
}

// TableName reports the bound table.
func (d *Driver) TableName() string { return d.cfg.TableName }

// Connect verifies the table exists via DescribeTable.
func (d *Driver) Connect(ctx context.Context) error {
	if d == nil || d.client == nil {
		return errors.New("dynamodb: driver is not initialized")
	}
	_, err := d.client.DescribeTable(ctx, &awsddb.DescribeTableInput{
		TableName: aws.String(d.cfg.TableName),
	})
	if err != nil {
		return fmt.Errorf("dynamodb: DescribeTable %s: %w", d.cfg.TableName, err)
	}
	return nil
}

// Scan returns up to `limit` items as Go-native maps. Limit ≤ 0
// defaults to 50; clamped to 1000 (DynamoDB hard cap).
func (d *Driver) Scan(ctx context.Context, limit int32) ([]map[string]any, error) {
	if d == nil || d.client == nil {
		return nil, errors.New("dynamodb: driver is not initialized")
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 1000 {
		limit = 1000
	}
	out, err := d.client.Scan(ctx, &awsddb.ScanInput{
		TableName: aws.String(d.cfg.TableName),
		Limit:     aws.Int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("dynamodb: Scan: %w", err)
	}
	rows := make([]map[string]any, 0, len(out.Items))
	for _, item := range out.Items {
		row := map[string]any{}
		if err := attributevalue.UnmarshalMap(item, &row); err != nil {
			return nil, fmt.Errorf("dynamodb: decode item: %w", err)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// GetItem fetches one item by its primary key (single-attribute
// HASH key). Returns (nil, nil) when the key is absent so callers
// can distinguish "missing" from "error".
func (d *Driver) GetItem(ctx context.Context, keyAttr string, keyValue string) (map[string]any, error) {
	if d == nil || d.client == nil {
		return nil, errors.New("dynamodb: driver is not initialized")
	}
	out, err := d.client.GetItem(ctx, &awsddb.GetItemInput{
		TableName: aws.String(d.cfg.TableName),
		Key: map[string]ddbtypes.AttributeValue{
			keyAttr: &ddbtypes.AttributeValueMemberS{Value: keyValue},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("dynamodb: GetItem: %w", err)
	}
	if len(out.Item) == 0 {
		return nil, nil
	}
	row := map[string]any{}
	if err := attributevalue.UnmarshalMap(out.Item, &row); err != nil {
		return nil, fmt.Errorf("dynamodb: decode item: %w", err)
	}
	return row, nil
}

// PutItem writes a Go-native map back to the table. Supports the
// primitives Marshal supports (string, number, bool, slices, maps).
func (d *Driver) PutItem(ctx context.Context, item map[string]any) error {
	if d == nil || d.client == nil {
		return errors.New("dynamodb: driver is not initialized")
	}
	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return fmt.Errorf("dynamodb: encode item: %w", err)
	}
	if _, err := d.client.PutItem(ctx, &awsddb.PutItemInput{
		TableName: aws.String(d.cfg.TableName),
		Item:      av,
	}); err != nil {
		return fmt.Errorf("dynamodb: PutItem: %w", err)
	}
	return nil
}
