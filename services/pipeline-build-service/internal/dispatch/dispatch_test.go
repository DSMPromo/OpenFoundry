package dispatch_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	pipelineplan "github.com/openfoundry/openfoundry-go/libs/pipeline-plan"
	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/dispatch"
)

// validPlan returns the smallest valid pipelineplan.Plan: one
// read_table → write_table. Used by every dispatcher test that needs
// validateInput to pass.
func validPlan() pipelineplan.Plan {
	return pipelineplan.Plan{
		PipelineID: "p", RunID: "r",
		Ops: []pipelineplan.Op{
			{
				ID: "src", Kind: pipelineplan.KindReadTable,
				ReadTable: &pipelineplan.ReadTable{Catalog: "c", Namespace: "n", Table: "t"},
			},
			{
				ID: "sink", Kind: pipelineplan.KindWriteTable, Inputs: []string{"src"},
				WriteTable: &pipelineplan.WriteTable{
					Catalog: "c", Namespace: "n", Table: "t",
					Mode: pipelineplan.WriteModeCreateOrReplace,
				},
			},
		},
	}
}

func validInput() dispatch.PipelineRunInput {
	return dispatch.PipelineRunInput{
		PipelineID:          "pipeline-1",
		RunID:               "run-1",
		Namespace:           "openfoundry",
		PipelineRunnerImage: "registry.example/pipeline-runner:0.1.0",
		InputDatasetRID:     "ds-in",
		OutputDatasetRID:    "ds-out",
		Plan:                validPlan(),
	}
}

func TestRenderManifest_emitsBatchV1Job(t *testing.T) {
	t.Parallel()
	manifest, err := dispatch.RenderManifest(validInput())
	require.NoError(t, err)
	require.Equal(t, "batch/v1", manifest["apiVersion"])
	require.Equal(t, "Job", manifest["kind"])

	meta := manifest["metadata"].(map[string]any)
	require.Equal(t, "openfoundry", meta["namespace"])
	require.True(t, strings.HasPrefix(meta["name"].(string), "pipeline-run-pipeline-1-run-1"),
		"name prefix wrong: %v", meta["name"])

	spec := manifest["spec"].(map[string]any)
	require.EqualValues(t, 0, spec["backoffLimit"], "Job should not retry on its own — runner owns the retry")
	require.EqualValues(t, 1800, spec["ttlSecondsAfterFinished"])

	pod := spec["template"].(map[string]any)["spec"].(map[string]any)
	require.Equal(t, "Never", pod["restartPolicy"])
	require.Equal(t, "pipeline-runner", pod["serviceAccountName"], "default service account is `pipeline-runner`")

	containers := pod["containers"].([]any)
	require.Len(t, containers, 1)
	c := containers[0].(map[string]any)
	require.Equal(t, "registry.example/pipeline-runner:0.1.0", c["image"])
	require.Equal(t, "Always", c["imagePullPolicy"])

	args := c["args"].([]any)
	require.Contains(t, args, "--pipeline-id")
	require.Contains(t, args, "pipeline-1")
	require.Contains(t, args, "--run-id")
	require.Contains(t, args, "run-1")
}

func TestRenderManifest_planRoundTripsViaEnvB64(t *testing.T) {
	t.Parallel()
	input := validInput()
	manifest, err := dispatch.RenderManifest(input)
	require.NoError(t, err)
	envList := manifest["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)["containers"].([]any)[0].(map[string]any)["env"].([]any)
	var planEnv map[string]any
	for _, e := range envList {
		em := e.(map[string]any)
		if em["name"] == "PIPELINE_PLAN_B64" {
			planEnv = em
			break
		}
	}
	require.NotNil(t, planEnv, "PIPELINE_PLAN_B64 env var missing")
	raw, err := base64.StdEncoding.DecodeString(planEnv["value"].(string))
	require.NoError(t, err)
	var roundTripped pipelineplan.Plan
	require.NoError(t, json.Unmarshal(raw, &roundTripped))
	require.Nil(t, roundTripped.Validate(), "round-tripped plan must validate cleanly")
	require.Equal(t, input.Plan, roundTripped)
}

func TestRenderManifest_rejectsMissingFields(t *testing.T) {
	t.Parallel()
	for _, missing := range []string{"pipeline_id", "run_id", "namespace", "pipeline_runner_image", "input_dataset_rid", "output_dataset_rid"} {
		missing := missing
		t.Run(missing, func(t *testing.T) {
			t.Parallel()
			input := validInput()
			switch missing {
			case "pipeline_id":
				input.PipelineID = ""
			case "run_id":
				input.RunID = ""
			case "namespace":
				input.Namespace = ""
			case "pipeline_runner_image":
				input.PipelineRunnerImage = ""
			case "input_dataset_rid":
				input.InputDatasetRID = ""
			case "output_dataset_rid":
				input.OutputDatasetRID = ""
			}
			_, err := dispatch.RenderManifest(input)
			require.Error(t, err)
			var invalid *dispatch.InvalidInputError
			require.True(t, errors.As(err, &invalid), "want *InvalidInputError")
			require.Contains(t, err.Error(), missing)
		})
	}
}

func TestRenderManifest_rejectsInvalidPlan(t *testing.T) {
	t.Parallel()
	input := validInput()
	input.Plan = pipelineplan.Plan{} // empty → invalid
	_, err := dispatch.RenderManifest(input)
	require.Error(t, err)
	var invalid *dispatch.InvalidInputError
	require.True(t, errors.As(err, &invalid))
	require.Contains(t, err.Error(), "plan invalid")
}

func TestRenderManifest_overridesResources(t *testing.T) {
	t.Parallel()
	input := validInput()
	input.Resources = dispatch.ResourceOverrides{CPULimit: "2", MemoryLimit: "2Gi"}
	manifest, err := dispatch.RenderManifest(input)
	require.NoError(t, err)
	c := manifest["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)["containers"].([]any)[0].(map[string]any)
	limits := c["resources"].(map[string]any)["limits"].(map[string]any)
	require.Equal(t, "2", limits["cpu"])
	require.Equal(t, "2Gi", limits["memory"])
}

func TestJobName_capsLength(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("a", 40)
	_, err := dispatch.JobName(long, long)
	require.Error(t, err)
	var invalid *dispatch.InvalidInputError
	require.True(t, errors.As(err, &invalid))
	require.Contains(t, err.Error(), "exceeds")
}

func TestKubernetesClient_SubmitPostsToBatchV1Path(t *testing.T) {
	t.Parallel()
	var gotPath, gotMethod string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"metadata":{"name":"pipeline-run-pipeline-1-run-1"}}`))
	}))
	defer srv.Close()

	cli, err := dispatch.NewKubernetesClient(srv.URL, "", srv.Client())
	require.NoError(t, err)
	name, err := cli.SubmitPipelineRun(context.Background(), validInput())
	require.NoError(t, err)
	require.Equal(t, "pipeline-run-pipeline-1-run-1", name)
	require.Equal(t, http.MethodPost, gotMethod)
	require.Equal(t, "/apis/batch/v1/namespaces/openfoundry/jobs", gotPath)

	// Body must be the rendered Job manifest.
	var posted map[string]any
	require.NoError(t, json.Unmarshal(gotBody, &posted))
	require.Equal(t, "batch/v1", posted["apiVersion"])
	require.Equal(t, "Job", posted["kind"])
}

func TestKubernetesClient_GetStatusMapsJobConditions(t *testing.T) {
	t.Parallel()
	// Succeeded: succeeded=1, no Failed condition.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":{"succeeded":1}}`))
	}))
	defer srv.Close()
	cli, err := dispatch.NewKubernetesClient(srv.URL, "", srv.Client())
	require.NoError(t, err)
	rep, err := cli.GetPipelineRunStatus(context.Background(), "openfoundry", "pipeline-run-x-y")
	require.NoError(t, err)
	require.Equal(t, dispatch.RunSucceeded, rep.Status)
}

func TestParseStatus_branches(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		body   string
		want   dispatch.RunStatus
		errMsg string
	}{
		"no status → submitted":    {`{}`, dispatch.RunSubmitted, ""},
		"empty status → submitted": {`{"status":{}}`, dispatch.RunSubmitted, ""},
		"active → running":         {`{"status":{"active":2}}`, dispatch.RunRunning, ""},
		"succeeded → succeeded":    {`{"status":{"succeeded":1}}`, dispatch.RunSucceeded, ""},
		"failed condition":         {`{"status":{"conditions":[{"type":"Failed","status":"True","message":"backoff"}]}}`, dispatch.RunFailed, "backoff"},
		"failed condition without message": {
			`{"status":{"conditions":[{"type":"Failed","status":"True"}]}}`, dispatch.RunFailed, "",
		},
		"failed condition status False is ignored": {
			`{"status":{"conditions":[{"type":"Failed","status":"False"}],"succeeded":1}}`, dispatch.RunSucceeded, "",
		},
	}
	for name, tc := range cases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var obj map[string]any
			require.NoError(t, json.Unmarshal([]byte(tc.body), &obj))
			got := dispatch.ParseStatus(obj)
			require.Equal(t, tc.want, got.Status)
			if tc.errMsg != "" {
				require.NotNil(t, got.ErrorMessage)
				require.Equal(t, tc.errMsg, *got.ErrorMessage)
			}
		})
	}
}

func TestKubernetesClient_GetStatus_NotFoundReturnsNil(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	cli, _ := dispatch.NewKubernetesClient(srv.URL, "", srv.Client())
	rep, err := cli.GetPipelineRunStatus(context.Background(), "ns", "name")
	require.NoError(t, err)
	require.Nil(t, rep, "404 must surface as (nil, nil) so the caller can treat it as 'unknown'")
}
