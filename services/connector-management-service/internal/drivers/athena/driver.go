// Package athena is the runtime client for the AWS Athena connector.
// Athena is asynchronous — StartQueryExecution returns an ID, the
// driver then polls GetQueryExecution until the state settles, and
// finally pulls rows via GetQueryResults. The Run helper folds those
// three steps into one call for the connector adapter.
package athena

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsathena "github.com/aws/aws-sdk-go-v2/service/athena"
	athenatypes "github.com/aws/aws-sdk-go-v2/service/athena/types"

	awsclient "github.com/openfoundry/openfoundry-go/libs/aws-client"
)

// Config carries Athena workgroup + result-bucket settings and the
// usual AWS connection knobs.
type Config struct {
	// WorkGroup is the Athena workgroup the query runs in.
	// Defaults to "primary" when empty.
	WorkGroup string `json:"workgroup"`

	// Database is the Glue database the query targets. Optional when
	// queries fully qualify their tables.
	Database string `json:"database"`

	// OutputLocation is the s3:// URI Athena writes results into.
	// Required by the AWS API unless the workgroup pins it.
	OutputLocation string `json:"output_location"`

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
			return cfg, fmt.Errorf("athena: invalid config: %w", err)
		}
	}
	if strings.TrimSpace(cfg.WorkGroup) == "" {
		cfg.WorkGroup = "primary"
	}
	return cfg, nil
}

// QueryResult is the projected shape of one Athena query result.
type QueryResult struct {
	Columns          []string            `json:"columns"`
	Rows             []map[string]string `json:"rows"`
	QueryExecutionID string              `json:"query_execution_id"`
}

// athenaClient is the minimum SDK surface the driver consumes.
type athenaClient interface {
	GetWorkGroup(ctx context.Context, in *awsathena.GetWorkGroupInput, opts ...func(*awsathena.Options)) (*awsathena.GetWorkGroupOutput, error)
	StartQueryExecution(ctx context.Context, in *awsathena.StartQueryExecutionInput, opts ...func(*awsathena.Options)) (*awsathena.StartQueryExecutionOutput, error)
	GetQueryExecution(ctx context.Context, in *awsathena.GetQueryExecutionInput, opts ...func(*awsathena.Options)) (*awsathena.GetQueryExecutionOutput, error)
	GetQueryResults(ctx context.Context, in *awsathena.GetQueryResultsInput, opts ...func(*awsathena.Options)) (*awsathena.GetQueryResultsOutput, error)
}

type Driver struct {
	cfg    Config
	client athenaClient
	// PollInterval governs how often Run polls GetQueryExecution.
	// 0 = use the default of 250ms. Tests override.
	PollInterval time.Duration
}

func New(ctx context.Context, cfg Config) (*Driver, error) {
	client, err := awsclient.Athena(ctx, awsclient.Config{
		EndpointURL:     cfg.Endpoint,
		Region:          cfg.Region,
		AccessKeyID:     cfg.AccessKeyID,
		SecretAccessKey: cfg.SecretAccessKey,
		SessionToken:    cfg.SessionToken,
	})
	if err != nil {
		return nil, fmt.Errorf("athena: %w", err)
	}
	return &Driver{cfg: cfg, client: client}, nil
}

// NewWithClient is the test-friendly constructor.
func NewWithClient(cfg Config, client athenaClient) *Driver {
	return &Driver{cfg: cfg, client: client}
}

// Connect verifies the workgroup is reachable via GetWorkGroup —
// cheap, doesn't touch S3 or Glue.
func (d *Driver) Connect(ctx context.Context) error {
	if d == nil || d.client == nil {
		return errors.New("athena: driver is not initialized")
	}
	_, err := d.client.GetWorkGroup(ctx, &awsathena.GetWorkGroupInput{
		WorkGroup: aws.String(d.cfg.WorkGroup),
	})
	if err != nil {
		return fmt.Errorf("athena: GetWorkGroup %s: %w", d.cfg.WorkGroup, err)
	}
	return nil
}

// Run executes sql synchronously: starts the query, polls until it
// settles, then fetches the result rows. The context's deadline
// caps the polling loop; long-running queries should set a generous
// timeout (Athena queries can take minutes).
func (d *Driver) Run(ctx context.Context, sql string) (*QueryResult, error) {
	if d == nil || d.client == nil {
		return nil, errors.New("athena: driver is not initialized")
	}
	start, err := d.client.StartQueryExecution(ctx, d.startInput(sql))
	if err != nil {
		return nil, fmt.Errorf("athena: StartQueryExecution: %w", err)
	}
	execID := aws.ToString(start.QueryExecutionId)

	if err := d.waitForCompletion(ctx, execID); err != nil {
		return nil, err
	}

	out, err := d.client.GetQueryResults(ctx, &awsathena.GetQueryResultsInput{
		QueryExecutionId: aws.String(execID),
	})
	if err != nil {
		return nil, fmt.Errorf("athena: GetQueryResults: %w", err)
	}
	return projectResults(out, execID), nil
}

func (d *Driver) startInput(sql string) *awsathena.StartQueryExecutionInput {
	in := &awsathena.StartQueryExecutionInput{
		QueryString: aws.String(sql),
		WorkGroup:   aws.String(d.cfg.WorkGroup),
	}
	if d.cfg.Database != "" {
		in.QueryExecutionContext = &athenatypes.QueryExecutionContext{
			Database: aws.String(d.cfg.Database),
		}
	}
	if d.cfg.OutputLocation != "" {
		in.ResultConfiguration = &athenatypes.ResultConfiguration{
			OutputLocation: aws.String(d.cfg.OutputLocation),
		}
	}
	return in
}

func (d *Driver) waitForCompletion(ctx context.Context, execID string) error {
	interval := d.PollInterval
	if interval <= 0 {
		interval = 250 * time.Millisecond
	}
	for {
		out, err := d.client.GetQueryExecution(ctx, &awsathena.GetQueryExecutionInput{
			QueryExecutionId: aws.String(execID),
		})
		if err != nil {
			return fmt.Errorf("athena: GetQueryExecution: %w", err)
		}
		state := athenatypes.QueryExecutionStateQueued
		if out.QueryExecution != nil && out.QueryExecution.Status != nil {
			state = out.QueryExecution.Status.State
		}
		switch state {
		case athenatypes.QueryExecutionStateSucceeded:
			return nil
		case athenatypes.QueryExecutionStateFailed, athenatypes.QueryExecutionStateCancelled:
			reason := ""
			if out.QueryExecution != nil && out.QueryExecution.Status != nil && out.QueryExecution.Status.StateChangeReason != nil {
				reason = aws.ToString(out.QueryExecution.Status.StateChangeReason)
			}
			return fmt.Errorf("athena: query %s: %s", strings.ToLower(string(state)), reason)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("athena: timed out waiting for query %s: %w", execID, ctx.Err())
		case <-time.After(interval):
		}
	}
}

// projectResults flattens Athena's columnar response into a slice of
// row maps where every cell is the string the AWS API returned.
// The first row of `ResultSet.Rows` is always the column-name row
// from Athena, so we skip it.
func projectResults(out *awsathena.GetQueryResultsOutput, execID string) *QueryResult {
	res := &QueryResult{QueryExecutionID: execID}
	if out == nil || out.ResultSet == nil {
		return res
	}
	rs := out.ResultSet
	if rs.ResultSetMetadata != nil {
		for _, ci := range rs.ResultSetMetadata.ColumnInfo {
			res.Columns = append(res.Columns, aws.ToString(ci.Name))
		}
	}
	// The Athena API returns the column-name row as the first entry
	// in Rows[]; skip it so result rows align with `res.Columns`.
	start := 0
	if len(rs.Rows) > 0 && len(rs.Rows[0].Data) == len(res.Columns) {
		header := true
		for i, d := range rs.Rows[0].Data {
			if aws.ToString(d.VarCharValue) != res.Columns[i] {
				header = false
				break
			}
		}
		if header {
			start = 1
		}
	}
	for _, r := range rs.Rows[start:] {
		row := make(map[string]string, len(res.Columns))
		for i, d := range r.Data {
			if i >= len(res.Columns) {
				break
			}
			row[res.Columns[i]] = aws.ToString(d.VarCharValue)
		}
		res.Rows = append(res.Rows, row)
	}
	return res
}
