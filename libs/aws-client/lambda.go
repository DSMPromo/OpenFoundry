package awsclient

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
)

// Lambda returns a ready-to-use *lambda.Client configured from the
// Config. The endpoint hook lets a service point at LocalStack
// Community (which DOES include Lambda, unlike Bedrock) for end-to-
// end dev runs against the openfoundry-echo fixture from
// infra/compose/local/localstack-init/localstack-seed.sh.
func Lambda(ctx context.Context, c Config) (*lambda.Client, error) {
	awsCfg, err := c.Build(ctx)
	if err != nil {
		return nil, err
	}
	opts := []func(*lambda.Options){}
	if c.UsesCustomEndpoint() {
		ep := c.EndpointURL
		opts = append(opts, func(o *lambda.Options) { o.BaseEndpoint = aws.String(ep) })
	}
	return lambda.NewFromConfig(awsCfg, opts...), nil
}
