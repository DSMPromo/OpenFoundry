package awsclient

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
)

// SNS returns a ready-to-use *sns.Client configured from the Config.
// EndpointURL routes through LocalStack Community — the
// openfoundry-broadcast topic seeded by Phase 2 is the end-to-end
// target.
func SNS(ctx context.Context, c Config) (*sns.Client, error) {
	awsCfg, err := c.Build(ctx)
	if err != nil {
		return nil, err
	}
	opts := []func(*sns.Options){}
	if c.UsesCustomEndpoint() {
		ep := c.EndpointURL
		opts = append(opts, func(o *sns.Options) { o.BaseEndpoint = aws.String(ep) })
	}
	return sns.NewFromConfig(awsCfg, opts...), nil
}
