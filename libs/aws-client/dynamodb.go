package awsclient

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
)

// DynamoDB returns a ready-to-use *dynamodb.Client configured from
// the Config. EndpointURL routes through LocalStack Community — the
// openfoundry-kv table seeded by Phase 2 is the end-to-end target.
func DynamoDB(ctx context.Context, c Config) (*dynamodb.Client, error) {
	awsCfg, err := c.Build(ctx)
	if err != nil {
		return nil, err
	}
	opts := []func(*dynamodb.Options){}
	if c.UsesCustomEndpoint() {
		ep := c.EndpointURL
		opts = append(opts, func(o *dynamodb.Options) { o.BaseEndpoint = aws.String(ep) })
	}
	return dynamodb.NewFromConfig(awsCfg, opts...), nil
}
