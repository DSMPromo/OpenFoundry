package awsclient

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3 returns a ready-to-use *s3.Client configured from the Config.
//
// When Config.EndpointURL is set the client is wired through that
// endpoint and S3 path-style addressing is enabled automatically
// (LocalStack, MinIO, and Ceph RGW all require it). PathStyle=true
// also forces path-style explicitly even on real AWS, for operators
// who need the bucket-in-path URL form.
func S3(ctx context.Context, c Config) (*awss3.Client, error) {
	awsCfg, err := c.Build(ctx)
	if err != nil {
		return nil, err
	}
	opts := []func(*awss3.Options){}
	if c.UsesCustomEndpoint() {
		ep := c.EndpointURL
		opts = append(opts, func(o *awss3.Options) { o.BaseEndpoint = aws.String(ep) })
	}
	if c.PathStyle || c.UsesCustomEndpoint() {
		opts = append(opts, func(o *awss3.Options) { o.UsePathStyle = true })
	}
	return awss3.NewFromConfig(awsCfg, opts...), nil
}
