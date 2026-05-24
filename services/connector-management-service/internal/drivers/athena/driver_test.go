package athena

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsathena "github.com/aws/aws-sdk-go-v2/service/athena"
	athenatypes "github.com/aws/aws-sdk-go-v2/service/athena/types"
)

type fakeAthenaClient struct {
	getWorkGroupErr error
	startErr        error
	startResp       *awsathena.StartQueryExecutionOutput
	startInput      *awsathena.StartQueryExecutionInput
	execStates      []athenatypes.QueryExecutionState
	execReason      string
	resultResp      *awsathena.GetQueryResultsOutput
	resultErr       error
}

func (f *fakeAthenaClient) GetWorkGroup(_ context.Context, _ *awsathena.GetWorkGroupInput, _ ...func(*awsathena.Options)) (*awsathena.GetWorkGroupOutput, error) {
	if f.getWorkGroupErr != nil {
		return nil, f.getWorkGroupErr
	}
	return &awsathena.GetWorkGroupOutput{}, nil
}

func (f *fakeAthenaClient) StartQueryExecution(_ context.Context, in *awsathena.StartQueryExecutionInput, _ ...func(*awsathena.Options)) (*awsathena.StartQueryExecutionOutput, error) {
	f.startInput = in
	if f.startErr != nil {
		return nil, f.startErr
	}
	if f.startResp != nil {
		return f.startResp, nil
	}
	return &awsathena.StartQueryExecutionOutput{QueryExecutionId: aws.String("q-1")}, nil
}

func (f *fakeAthenaClient) GetQueryExecution(_ context.Context, _ *awsathena.GetQueryExecutionInput, _ ...func(*awsathena.Options)) (*awsathena.GetQueryExecutionOutput, error) {
	state := athenatypes.QueryExecutionStateSucceeded
	if len(f.execStates) > 0 {
		state = f.execStates[0]
		f.execStates = f.execStates[1:]
	}
	return &awsathena.GetQueryExecutionOutput{
		QueryExecution: &athenatypes.QueryExecution{
			Status: &athenatypes.QueryExecutionStatus{
				State:             state,
				StateChangeReason: aws.String(f.execReason),
			},
		},
	}, nil
}

func (f *fakeAthenaClient) GetQueryResults(_ context.Context, _ *awsathena.GetQueryResultsInput, _ ...func(*awsathena.Options)) (*awsathena.GetQueryResultsOutput, error) {
	if f.resultErr != nil {
		return nil, f.resultErr
	}
	if f.resultResp != nil {
		return f.resultResp, nil
	}
	return &awsathena.GetQueryResultsOutput{}, nil
}

func TestConfigFromJSONDefaultsWorkgroup(t *testing.T) {
	cfg, err := ConfigFromJSON(json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WorkGroup != "primary" {
		t.Errorf("workgroup default = %q, want 'primary'", cfg.WorkGroup)
	}
}

func TestConnectWrapsSDKError(t *testing.T) {
	client := &fakeAthenaClient{getWorkGroupErr: errors.New("AccessDeniedException")}
	d := NewWithClient(Config{WorkGroup: "primary"}, client)
	err := d.Connect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "AccessDeniedException") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunPollsThenReturnsProjectedRows(t *testing.T) {
	client := &fakeAthenaClient{
		execStates: []athenatypes.QueryExecutionState{
			athenatypes.QueryExecutionStateQueued,
			athenatypes.QueryExecutionStateRunning,
			athenatypes.QueryExecutionStateSucceeded,
		},
		resultResp: athenaResultFixture(),
	}
	d := NewWithClient(Config{WorkGroup: "primary", Database: "ops"}, client)
	d.PollInterval = 1 * time.Millisecond
	got, err := d.Run(context.Background(), "SELECT id, name FROM users")
	if err != nil {
		t.Fatal(err)
	}
	if got.QueryExecutionID != "q-1" {
		t.Errorf("execID = %q", got.QueryExecutionID)
	}
	if len(got.Columns) != 2 || got.Columns[0] != "id" || got.Columns[1] != "name" {
		t.Fatalf("columns wrong: %+v", got.Columns)
	}
	if len(got.Rows) != 2 || got.Rows[0]["id"] != "1" || got.Rows[1]["name"] != "Bea" {
		t.Fatalf("rows wrong: %+v", got.Rows)
	}
	// Database routed into the start input.
	if got := aws.ToString(client.startInput.QueryExecutionContext.Database); got != "ops" {
		t.Errorf("database not routed: %q", got)
	}
}

func TestRunFailsOnQueryFailedState(t *testing.T) {
	client := &fakeAthenaClient{
		execStates: []athenatypes.QueryExecutionState{athenatypes.QueryExecutionStateFailed},
		execReason: "syntax error near 'FRoM'",
	}
	d := NewWithClient(Config{WorkGroup: "primary"}, client)
	d.PollInterval = 1 * time.Millisecond
	_, err := d.Run(context.Background(), "SELECT 1")
	if err == nil || !strings.Contains(err.Error(), "syntax error") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunRespectsContextCancellation(t *testing.T) {
	client := &fakeAthenaClient{
		execStates: []athenatypes.QueryExecutionState{
			athenatypes.QueryExecutionStateQueued,
			athenatypes.QueryExecutionStateQueued,
			athenatypes.QueryExecutionStateQueued,
		},
	}
	d := NewWithClient(Config{WorkGroup: "primary"}, client)
	d.PollInterval = 10 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	_, err := d.Run(ctx, "SELECT 1")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v, want timeout", err)
	}
}

func athenaResultFixture() *awsathena.GetQueryResultsOutput {
	col := func(name string) athenatypes.ColumnInfo {
		return athenatypes.ColumnInfo{Name: aws.String(name)}
	}
	cell := func(v string) athenatypes.Datum {
		return athenatypes.Datum{VarCharValue: aws.String(v)}
	}
	return &awsathena.GetQueryResultsOutput{
		ResultSet: &athenatypes.ResultSet{
			ResultSetMetadata: &athenatypes.ResultSetMetadata{
				ColumnInfo: []athenatypes.ColumnInfo{col("id"), col("name")},
			},
			Rows: []athenatypes.Row{
				// header row (Athena always includes it)
				{Data: []athenatypes.Datum{cell("id"), cell("name")}},
				{Data: []athenatypes.Datum{cell("1"), cell("Ada")}},
				{Data: []athenatypes.Datum{cell("2"), cell("Bea")}},
			},
		},
	}
}
