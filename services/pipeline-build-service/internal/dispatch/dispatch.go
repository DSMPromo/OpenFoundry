// Package dispatch renders Kubernetes Job manifests for pipeline-runner
// invocations and submits them via a dynamic HTTP Kubernetes client.
// ADR-0045 Phase C.4.a — replaces the prior `spark` package that
// rendered SparkApplication CRs against the Spark Operator. The runner
// container itself is now the Go binary in services/pipeline-runner
// (C.5 swaps its implementation to consume a [pipelineplan.Plan]).
package dispatch

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	pipelineplan "github.com/openfoundry/openfoundry-go/libs/pipeline-plan"
)

const (
	// JobGroup / JobVersion / JobKind / JobPlural — Kubernetes
	// batch/v1 Job resource. Used to build apiserver URLs that submit
	// runs and poll their status.
	JobGroup   = "batch"
	JobVersion = "v1"
	JobKind    = "Job"
	JobPlural  = "jobs"
	// MaxJobNameLen — `name` field in batch/v1 Job objects must be a
	// DNS-1035 label (≤ 63 chars). We reserve some headroom for the
	// `pipeline-run-` prefix and the hyphenated `<pipelineID>-<runID>`
	// tail, mirroring the cap the previous SparkApplication renderer
	// used.
	MaxJobNameLen = 50
)

const pipelineRunTemplate = `apiVersion: batch/v1
kind: Job
metadata:
  name: ${job_name}
  namespace: ${namespace}
  labels:
    openfoundry.io/component: pipeline-runner
    openfoundry.io/pipeline-id: "${pipeline_id}"
    openfoundry.io/run-id: "${run_id}"
spec:
  backoffLimit: 0
  ttlSecondsAfterFinished: 1800
  template:
    metadata:
      labels:
        openfoundry.io/component: pipeline-runner
        openfoundry.io/pipeline-id: "${pipeline_id}"
        openfoundry.io/run-id: "${run_id}"
    spec:
      restartPolicy: Never
      serviceAccountName: ${service_account}
      containers:
        - name: pipeline-runner
          image: ${pipeline_runner_image}
          imagePullPolicy: Always
          args:
            - "--pipeline-id"
            - "${pipeline_id}"
            - "--run-id"
            - "${run_id}"
            - "--input-dataset"
            - "${input_dataset_rid}"
            - "--output-dataset"
            - "${output_dataset_rid}"
          env:
            - name: PIPELINE_PLAN_B64
              value: "${plan_b64}"
            - name: AWS_ACCESS_KEY_ID
              valueFrom:
                secretKeyRef:
                  name: openfoundry-iceberg
                  key: AWS_ACCESS_KEY_ID
                  optional: true
            - name: AWS_SECRET_ACCESS_KEY
              valueFrom:
                secretKeyRef:
                  name: openfoundry-iceberg
                  key: AWS_SECRET_ACCESS_KEY
                  optional: true
          resources:
            requests:
              cpu: "${cpu_request}"
              memory: "${memory_request}"
            limits:
              cpu: "${cpu_limit}"
              memory: "${memory_limit}"
          securityContext:
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            runAsNonRoot: true
            runAsUser: 65532
            capabilities:
              drop: ["ALL"]
`

// ResourceOverrides shapes the Job's container resource requests and
// limits. Mirrors the prior SparkResourceOverrides shape so authoring
// configs that already set these knobs continue to work.
type ResourceOverrides struct {
	CPURequest    string `json:"cpu_request,omitempty"`
	MemoryRequest string `json:"memory_request,omitempty"`
	CPULimit      string `json:"cpu_limit,omitempty"`
	MemoryLimit   string `json:"memory_limit,omitempty"`
}

// DefaultResourceOverrides — modest defaults that match the dev k3s
// cluster's per-pod budget. Production should override via authoring
// config.
func DefaultResourceOverrides() ResourceOverrides {
	return ResourceOverrides{
		CPURequest:    "200m",
		MemoryRequest: "256Mi",
		CPULimit:      "1",
		MemoryLimit:   "1Gi",
	}
}

func (r ResourceOverrides) withDefaults() ResourceOverrides {
	d := DefaultResourceOverrides()
	if strings.TrimSpace(r.CPURequest) != "" {
		d.CPURequest = r.CPURequest
	}
	if strings.TrimSpace(r.MemoryRequest) != "" {
		d.MemoryRequest = r.MemoryRequest
	}
	if strings.TrimSpace(r.CPULimit) != "" {
		d.CPULimit = r.CPULimit
	}
	if strings.TrimSpace(r.MemoryLimit) != "" {
		d.MemoryLimit = r.MemoryLimit
	}
	return d
}

// PipelineRunInput is everything the dispatcher needs to submit one
// pipeline-runner Job. The Plan field is the typed operator graph the
// runner executes; pipeline-build-service composes it from the DAG
// node configs before dispatch.
type PipelineRunInput struct {
	PipelineID          string            `json:"pipeline_id"`
	RunID               string            `json:"run_id"`
	Namespace           string            `json:"namespace"`
	PipelineRunnerImage string            `json:"pipeline_runner_image"`
	ServiceAccount      string            `json:"service_account,omitempty"`
	InputDatasetRID     string            `json:"input_dataset_rid"`
	OutputDatasetRID    string            `json:"output_dataset_rid"`
	Resources           ResourceOverrides `json:"resources"`
	Plan                pipelineplan.Plan `json:"plan"`
}

// PipelineRunDefaults applies sensible defaults to optional fields.
// Centralised so handlers and tests stay in sync.
func PipelineRunDefaults(input PipelineRunInput) PipelineRunInput {
	if strings.TrimSpace(input.ServiceAccount) == "" {
		input.ServiceAccount = "pipeline-runner"
	}
	return input
}

// RunStatus is the dispatcher-level state machine derived from the
// underlying batch/v1 Job status. Mirrors the small set of states the
// previous SparkRunStatus exposed so the polling caller does not
// have to know about Kubernetes Job conditions.
type RunStatus string

const (
	RunSubmitted RunStatus = "SUBMITTED"
	RunRunning   RunStatus = "RUNNING"
	RunSucceeded RunStatus = "SUCCEEDED"
	RunFailed    RunStatus = "FAILED"
	RunUnknown   RunStatus = "UNKNOWN"
)

// RunStatusReport is what GetPipelineRunStatus returns. ErrorMessage
// is populated for RunFailed when the Job reports a condition message.
type RunStatusReport struct {
	Status       RunStatus `json:"status"`
	ErrorMessage *string   `json:"error_message,omitempty"`
}

// Client is the seam every caller talks to. The production impl is
// KubernetesClient (HTTP against the k8s apiserver); tests inject a
// fake.
type Client interface {
	SubmitPipelineRun(ctx context.Context, input PipelineRunInput) (string, error)
	GetPipelineRunStatus(ctx context.Context, namespace, name string) (*RunStatusReport, error)
}

// InvalidInputError signals a bad PipelineRunInput (missing fields,
// invalid Plan, etc.). Wraps the underlying ValidationErrors when the
// Plan failed pipelineplan.Validate().
type InvalidInputError struct{ Message string }

func (e *InvalidInputError) Error() string { return "invalid pipeline run input: " + e.Message }

// RenderError is a template / serialisation failure.
type RenderError struct{ Message string }

func (e *RenderError) Error() string { return "manifest rendering failed: " + e.Message }

// KubeError carries the kube apiserver response code and body for
// downstream classification (e.g. 404 → ErrJobNotFound semantics).
type KubeError struct {
	StatusCode int
	Message    string
}

func (e *KubeError) Error() string { return fmt.Sprintf("kubernetes api call failed: %s", e.Message) }

// UnavailableError signals the kube client could not be built (no
// kubeconfig, no in-cluster service account, etc.).
type UnavailableError struct{ Err error }

func (e *UnavailableError) Error() string { return "kubernetes client not configured" }
func (e *UnavailableError) Unwrap() error { return e.Err }

// RenderManifest produces the Job manifest as a map[string]any ready
// to JSON-marshal into the kube apiserver body. The Plan is encoded
// as base64-JSON and injected via the PIPELINE_PLAN_B64 env var so
// the runner can rehydrate it without a ConfigMap mount.
func RenderManifest(input PipelineRunInput) (map[string]any, error) {
	input = PipelineRunDefaults(input)
	input.Resources = input.Resources.withDefaults()
	if err := validateInput(input); err != nil {
		return nil, err
	}
	name, err := JobName(input.PipelineID, input.RunID)
	if err != nil {
		return nil, err
	}
	planJSON, err := json.Marshal(input.Plan)
	if err != nil {
		return nil, &RenderError{Message: "encode plan: " + err.Error()}
	}
	planB64 := base64.StdEncoding.EncodeToString(planJSON)

	vars := map[string]string{
		"job_name":              name,
		"pipeline_id":           input.PipelineID,
		"run_id":                input.RunID,
		"namespace":             input.Namespace,
		"service_account":       input.ServiceAccount,
		"pipeline_runner_image": input.PipelineRunnerImage,
		"input_dataset_rid":     input.InputDatasetRID,
		"output_dataset_rid":    input.OutputDatasetRID,
		"plan_b64":              planB64,
		"cpu_request":           input.Resources.CPURequest,
		"memory_request":        input.Resources.MemoryRequest,
		"cpu_limit":             input.Resources.CPULimit,
		"memory_limit":          input.Resources.MemoryLimit,
	}
	rendered := pipelineRunTemplate
	for k, v := range vars {
		rendered = strings.ReplaceAll(rendered, "${"+k+"}", v)
	}
	if missing := firstUnsubstitutedPlaceholder(rendered); missing != "" {
		return nil, &RenderError{Message: "unresolved placeholder ${" + missing + "} in pipeline-run template"}
	}
	var yamlObj any
	if err := yaml.Unmarshal([]byte(rendered), &yamlObj); err != nil {
		return nil, &RenderError{Message: "YAML parse: " + err.Error()}
	}
	jsonObj := normalizeYAML(yamlObj).(map[string]any)
	meta, _ := jsonObj["metadata"].(map[string]any)
	if meta == nil {
		meta = map[string]any{}
		jsonObj["metadata"] = meta
	}
	meta["name"] = name
	meta["namespace"] = input.Namespace
	return jsonObj, nil
}

// JobName derives the kube Job name from pipelineID + runID. Capped
// at MaxJobNameLen so kube acceptance validation does not surface as
// a confusing API error.
func JobName(pipelineID, runID string) (string, error) {
	name := fmt.Sprintf("pipeline-run-%s-%s", pipelineID, runID)
	if len(name) > MaxJobNameLen {
		return "", &InvalidInputError{Message: fmt.Sprintf("computed Job name %q (%d chars) exceeds the %d-char limit; truncate pipeline_id / run_id before submission", name, len(name), MaxJobNameLen)}
	}
	return name, nil
}

func validateInput(input PipelineRunInput) error {
	for label, value := range map[string]string{
		"pipeline_id":           input.PipelineID,
		"run_id":                input.RunID,
		"namespace":             input.Namespace,
		"pipeline_runner_image": input.PipelineRunnerImage,
		"input_dataset_rid":     input.InputDatasetRID,
		"output_dataset_rid":    input.OutputDatasetRID,
	} {
		if strings.TrimSpace(value) == "" {
			return &InvalidInputError{Message: label + " must not be empty"}
		}
	}
	if errs := input.Plan.Validate(); errs != nil {
		return &InvalidInputError{Message: "plan invalid: " + errs.Error()}
	}
	return nil
}

// KubernetesClient is the production Client. Submits Jobs and reads
// JobStatus over the kube apiserver HTTPS surface.
type KubernetesClient struct {
	BaseURL     string
	BearerToken string
	HTTPClient  *http.Client
}

func NewKubernetesClient(baseURL, bearerToken string, httpClient *http.Client) (*KubernetesClient, error) {
	if _, err := url.ParseRequestURI(baseURL); err != nil {
		return nil, err
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &KubernetesClient{BaseURL: strings.TrimRight(baseURL, "/"), BearerToken: bearerToken, HTTPClient: httpClient}, nil
}

func NewKubernetesClientFromEnv() (*KubernetesClient, error) {
	if explicit := os.Getenv("KUBERNETES_API_URL"); explicit != "" {
		return NewKubernetesClient(explicit, os.Getenv("KUBERNETES_BEARER_TOKEN"), nil)
	}
	if host, port := os.Getenv("KUBERNETES_SERVICE_HOST"), os.Getenv("KUBERNETES_SERVICE_PORT"); host != "" && port != "" {
		token, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token")
		if err != nil {
			return nil, &UnavailableError{Err: err}
		}
		return NewKubernetesClient("https://"+host+":"+port, strings.TrimSpace(string(token)),
			&http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}})
	}
	return clientFromKubeconfig()
}

func (c *KubernetesClient) SubmitPipelineRun(ctx context.Context, input PipelineRunInput) (string, error) {
	manifest, err := RenderManifest(input)
	if err != nil {
		return "", err
	}
	body, _ := json.Marshal(manifest)
	path := fmt.Sprintf("/apis/%s/%s/namespaces/%s/%s", JobGroup, JobVersion, input.Namespace, JobPlural)
	var created map[string]any
	if err := c.doJSON(ctx, http.MethodPost, path, body, &created); err != nil {
		return "", err
	}
	if meta, _ := created["metadata"].(map[string]any); meta != nil {
		if name, _ := meta["name"].(string); name != "" {
			return name, nil
		}
	}
	return JobName(input.PipelineID, input.RunID)
}

func (c *KubernetesClient) GetPipelineRunStatus(ctx context.Context, namespace, name string) (*RunStatusReport, error) {
	path := fmt.Sprintf("/apis/%s/%s/namespaces/%s/%s/%s", JobGroup, JobVersion, namespace, JobPlural, name)
	var obj map[string]any
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &obj); err != nil {
		var kube *KubeError
		if errors.As(err, &kube) && kube.StatusCode == http.StatusNotFound {
			return nil, nil
		}
		return nil, err
	}
	report := ParseStatus(obj)
	return &report, nil
}

// PodLogOptions is the subset of `corev1.PodLogOptions` the build-log
// tail needs. Follow=true mirrors `kubectl logs -f`. TailLines bounds
// the catch-up window when subscribing to a long-running pod so we
// don't dump megabytes before the live stream opens.
type PodLogOptions struct {
	Follow     bool
	TailLines  int64
	Container  string
	Timestamps bool
}

// StreamPodLogs returns an io.ReadCloser streaming the raw log bytes
// from the named pod. The caller MUST Close() the reader to release
// the HTTP connection. The K8s logs endpoint returns text/plain — no
// framing — so the caller is responsible for line splitting.
//
// Returns a *KubeError (StatusCode populated) when the apiserver
// rejects the request. A 404 there means "pod not found" — the live-
// log subscriber translates that into a clean "no logs yet" rather
// than tearing down the client connection.
func (c *KubernetesClient) StreamPodLogs(ctx context.Context, namespace, podName string, opts PodLogOptions) (io.ReadCloser, error) {
	query := url.Values{}
	if opts.Follow {
		query.Set("follow", "true")
	}
	if opts.TailLines > 0 {
		query.Set("tailLines", strconv.FormatInt(opts.TailLines, 10))
	}
	if opts.Container != "" {
		query.Set("container", opts.Container)
	}
	if opts.Timestamps {
		query.Set("timestamps", "true")
	}
	path := fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/log", namespace, podName)
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/plain")
	if c.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.BearerToken)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, &KubeError{Message: err.Error()}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, &KubeError{StatusCode: resp.StatusCode, Message: string(body)}
	}
	return resp.Body, nil
}

func (c *KubernetesClient) doJSON(ctx context.Context, method, path string, body []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.BearerToken)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return &KubeError{Message: err.Error()}
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &KubeError{StatusCode: resp.StatusCode, Message: string(respBody)}
	}
	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return &RenderError{Message: "kubernetes response JSON parse: " + err.Error()}
		}
	}
	return nil
}

// ParseStatus reads a batch/v1 Job status payload and maps it to the
// RunStatus state machine. Logic:
//
//   - status.succeeded > 0     → SUCCEEDED
//   - conditions[type=Failed].status=True
//     → FAILED (carries condition message)
//   - status.active > 0        → RUNNING
//   - otherwise                → SUBMITTED (Job created, no pod started yet)
//
// We treat absence of `status` as SUBMITTED — the apiserver returns
// the manifest before the controller has touched the status subresource.
func ParseStatus(body map[string]any) RunStatusReport {
	statusObj, _ := body["status"].(map[string]any)
	if statusObj == nil {
		return RunStatusReport{Status: RunSubmitted}
	}
	conditionFailed, failureMsg := scanFailedCondition(statusObj)
	if conditionFailed {
		var msg *string
		if failureMsg != "" {
			msg = &failureMsg
		}
		return RunStatusReport{Status: RunFailed, ErrorMessage: msg}
	}
	if numericFieldGreaterThanZero(statusObj, "succeeded") {
		return RunStatusReport{Status: RunSucceeded}
	}
	if numericFieldGreaterThanZero(statusObj, "active") {
		return RunStatusReport{Status: RunRunning}
	}
	return RunStatusReport{Status: RunSubmitted}
}

func scanFailedCondition(statusObj map[string]any) (bool, string) {
	conditions, _ := statusObj["conditions"].([]any)
	for _, c := range conditions {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if t, _ := cm["type"].(string); t != "Failed" {
			continue
		}
		if s, _ := cm["status"].(string); strings.EqualFold(s, "True") {
			msg, _ := cm["message"].(string)
			return true, msg
		}
	}
	return false, ""
}

func numericFieldGreaterThanZero(obj map[string]any, field string) bool {
	switch v := obj[field].(type) {
	case float64:
		return v > 0
	case int:
		return v > 0
	case int64:
		return v > 0
	}
	return false
}

func normalizeYAML(v any) any {
	switch x := v.(type) {
	case map[string]any:
		m := map[string]any{}
		for k, v := range x {
			m[k] = normalizeYAML(v)
		}
		return m
	case map[any]any:
		m := map[string]any{}
		for k, v := range x {
			m[fmt.Sprint(k)] = normalizeYAML(v)
		}
		return m
	case []any:
		for i, v := range x {
			x[i] = normalizeYAML(v)
		}
		return x
	default:
		return x
	}
}

func firstUnsubstitutedPlaceholder(s string) string {
	start := strings.Index(s, "${")
	for start >= 0 {
		rest := s[start+2:]
		end := strings.Index(rest, "}")
		if end < 0 {
			return ""
		}
		name := rest[:end]
		ok := name != ""
		for _, r := range name {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_') {
				ok = false
				break
			}
		}
		if ok {
			return name
		}
		next := strings.Index(rest[end+1:], "${")
		if next < 0 {
			return ""
		}
		start += 2 + end + 1 + next
	}
	return ""
}

type kubeconfig struct {
	CurrentContext string `yaml:"current-context"`
	Clusters       []struct {
		Name    string `yaml:"name"`
		Cluster struct {
			Server                   string `yaml:"server"`
			CertificateAuthority     string `yaml:"certificate-authority"`
			CertificateAuthorityData string `yaml:"certificate-authority-data"`
			InsecureSkipTLSVerify    bool   `yaml:"insecure-skip-tls-verify"`
		} `yaml:"cluster"`
	} `yaml:"clusters"`
	Users []struct {
		Name string `yaml:"name"`
		User struct {
			Token string `yaml:"token"`
		} `yaml:"user"`
	} `yaml:"users"`
	Contexts []struct {
		Name    string `yaml:"name"`
		Context struct {
			Cluster string `yaml:"cluster"`
			User    string `yaml:"user"`
		} `yaml:"context"`
	} `yaml:"contexts"`
}

func clientFromKubeconfig() (*KubernetesClient, error) {
	path := os.Getenv("KUBECONFIG")
	if path == "" {
		home, _ := os.UserHomeDir()
		if home != "" {
			path = filepath.Join(home, ".kube", "config")
		}
	}
	if path == "" {
		return nil, &UnavailableError{}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, &UnavailableError{Err: err}
	}
	var cfg kubeconfig
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, &UnavailableError{Err: err}
	}
	ctxName := cfg.CurrentContext
	if ctxName == "" && len(cfg.Contexts) > 0 {
		ctxName = cfg.Contexts[0].Name
	}
	var clusterName, userName string
	for _, ctx := range cfg.Contexts {
		if ctx.Name == ctxName {
			clusterName, userName = ctx.Context.Cluster, ctx.Context.User
			break
		}
	}
	var server, token string
	var selectedCluster *struct {
		Server                   string `yaml:"server"`
		CertificateAuthority     string `yaml:"certificate-authority"`
		CertificateAuthorityData string `yaml:"certificate-authority-data"`
		InsecureSkipTLSVerify    bool   `yaml:"insecure-skip-tls-verify"`
	}
	for i := range cfg.Clusters {
		if cfg.Clusters[i].Name == clusterName {
			server = cfg.Clusters[i].Cluster.Server
			selectedCluster = &cfg.Clusters[i].Cluster
			break
		}
	}
	for _, u := range cfg.Users {
		if u.Name == userName {
			token = u.User.Token
			break
		}
	}
	if server == "" {
		return nil, &UnavailableError{}
	}
	httpClient, err := httpClientFromKubeCluster(selectedCluster, filepath.Dir(path))
	if err != nil {
		return nil, &UnavailableError{Err: err}
	}
	return NewKubernetesClient(server, token, httpClient)
}

func httpClientFromKubeCluster(cluster *struct {
	Server                   string `yaml:"server"`
	CertificateAuthority     string `yaml:"certificate-authority"`
	CertificateAuthorityData string `yaml:"certificate-authority-data"`
	InsecureSkipTLSVerify    bool   `yaml:"insecure-skip-tls-verify"`
}, kubeconfigDir string,
) (*http.Client, error) {
	if cluster == nil {
		return nil, nil
	}
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if cluster.InsecureSkipTLSVerify {
		tlsCfg.InsecureSkipVerify = true //nolint:gosec // Mirrors explicit kubeconfig opt-out for local test clusters.
	}
	if strings.TrimSpace(cluster.CertificateAuthorityData) != "" {
		pemBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(cluster.CertificateAuthorityData))
		if err != nil {
			return nil, fmt.Errorf("decode kubeconfig certificate-authority-data: %w", err)
		}
		pool, err := certPoolFromPEM(pemBytes)
		if err != nil {
			return nil, err
		}
		tlsCfg.RootCAs = pool
	} else if strings.TrimSpace(cluster.CertificateAuthority) != "" {
		caPath := cluster.CertificateAuthority
		if !filepath.IsAbs(caPath) {
			caPath = filepath.Join(kubeconfigDir, caPath)
		}
		pemBytes, err := os.ReadFile(caPath)
		if err != nil {
			return nil, fmt.Errorf("read kubeconfig certificate-authority: %w", err)
		}
		pool, err := certPoolFromPEM(pemBytes)
		if err != nil {
			return nil, err
		}
		tlsCfg.RootCAs = pool
	}
	if tlsCfg.RootCAs == nil && !tlsCfg.InsecureSkipVerify {
		return nil, nil
	}
	return &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{TLSClientConfig: tlsCfg}}, nil
}

func certPoolFromPEM(pemBytes []byte) (*x509.CertPool, error) {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pemBytes) {
		return nil, errors.New("kubeconfig certificate authority did not contain a valid PEM certificate")
	}
	return pool, nil
}
