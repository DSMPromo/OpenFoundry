package awsclient

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/athena"
)

// Athena returns a ready-to-use *athena.Client configured from the
// Config. EndpointURL routes through LocalStack Community.
func Athena(ctx context.Context, c Config) (*athena.Client, error) {
	awsCfg, err := c.Build(ctx)
	if err != nil {
		return nil, err
	}
	opts := []func(*athena.Options){}
	if c.UsesCustomEndpoint() {
		ep := c.EndpointURL
		opts = append(opts, func(o *athena.Options) { o.BaseEndpoint = aws.String(ep) })
	}
	return athena.NewFromConfig(awsCfg, opts...), nil
}
