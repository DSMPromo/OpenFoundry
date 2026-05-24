package awsclient

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
)

// BedrockRuntime returns a ready-to-use *bedrockruntime.Client backed
// by the supplied Config. When Config.EndpointURL is set the client
// is wired through that endpoint — useful for LocalStack Pro and any
// future Bedrock-compatible local emulator. Real AWS is the default.
//
// Bedrock is region-scoped (model availability differs by region), so
// Config.Region SHOULD be set explicitly for production use; the
// fallback "us-east-1" the rest of awsclient applies is fine for
// local dev but a footgun for prod where you may want eu-central-1
// or us-west-2 instead.
func BedrockRuntime(ctx context.Context, c Config) (*bedrockruntime.Client, error) {
	awsCfg, err := c.Build(ctx)
	if err != nil {
		return nil, err
	}
	opts := []func(*bedrockruntime.Options){}
	if c.UsesCustomEndpoint() {
		ep := c.EndpointURL
		opts = append(opts, func(o *bedrockruntime.Options) { o.BaseEndpoint = aws.String(ep) })
	}
	return bedrockruntime.NewFromConfig(awsCfg, opts...), nil
}
