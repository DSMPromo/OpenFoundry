package awsclient

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

// SQS returns a ready-to-use *sqs.Client configured from the Config.
// EndpointURL routes through LocalStack Community — the
// openfoundry-pipeline-events and openfoundry-deadletter queues
// seeded by Phase 2 are the end-to-end targets.
func SQS(ctx context.Context, c Config) (*sqs.Client, error) {
	awsCfg, err := c.Build(ctx)
	if err != nil {
		return nil, err
	}
	opts := []func(*sqs.Options){}
	if c.UsesCustomEndpoint() {
		ep := c.EndpointURL
		opts = append(opts, func(o *sqs.Options) { o.BaseEndpoint = aws.String(ep) })
	}
	return sqs.NewFromConfig(awsCfg, opts...), nil
}
