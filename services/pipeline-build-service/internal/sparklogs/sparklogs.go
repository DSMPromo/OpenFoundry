// Package sparklogs adapts the kube apiserver pod-log streaming
// surface into the live-log ports (logs.LogStore / LogSubscriber) the
// HTTP handlers consume.
//
// The previous in-memory subscriber only sees lines the build-service
// emits to itself (via AppendLog). That covers the FASTER lightweight
// path but is silent for DISTRIBUTED Spark / pipeline-runner Jobs,
// where the actual driver runs inside a pod and writes to stdout.
// This package fills that gap: Subscribe opens a `kubectl logs -f`
// stream against the resolved pod and pushes each line as a LogEntry.
//
// A4.4 of TASKS_COMPUTE_PIPELINES.md.
package sparklogs

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/dispatch"
	livellogs "github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/logs"
)

// PodResolver maps a jobRID to the namespace+pod pair the driver
// container runs in. The production implementation looks the run up
// in pipeline_run_submissions; tests pass a static fake.
type PodResolver interface {
	ResolvePod(ctx context.Context, jobRID string) (namespace, pod string, err error)
}

// PodLogStreamer is the subset of dispatch.KubernetesClient the
// subscriber needs. Lets tests stand in a fake without exercising the
// rest of the kube apiserver surface.
type PodLogStreamer interface {
	StreamPodLogs(ctx context.Context, namespace, podName string, opts dispatch.PodLogOptions) (io.ReadCloser, error)
}

// PodSubscriber implements livellogs.LogSubscriber by streaming log
// bytes from the resolved pod. Each Subscribe call spawns a goroutine
// that reads lines from the kube apiserver and forwards them as
// LogEntry values on the returned channel. The cancel func closes the
// underlying HTTP stream so the goroutine returns cleanly.
type PodSubscriber struct {
	Client     PodLogStreamer
	Resolver   PodResolver
	Logger     *slog.Logger
	TailLines  int64  // 0 → server default (no tail)
	Container  string // empty → first container in the pod
	Timestamps bool   // if true, request `timestamps=true` from kube
}

// ErrNoPod signals that the resolver couldn't find a pod for the
// supplied jobRID. The subscriber surfaces this as a closed channel
// rather than a permanent error so the handler can degrade to "no
// live logs available yet" — the pod may still be pending.
var ErrNoPod = errors.New("sparklogs: no pod for job")

// Subscribe resolves the jobRID, opens a follow=true log stream
// against the resolved pod, and pushes lines through the returned
// channel. The returned cancel closes the upstream HTTP body and the
// channel; ctx cancellation triggers the same path.
func (p *PodSubscriber) Subscribe(ctx context.Context, jobRID string) (<-chan livellogs.LogEntry, func(), error) {
	if p == nil || p.Client == nil || p.Resolver == nil {
		return nil, nil, errors.New("sparklogs: PodSubscriber not configured")
	}
	namespace, pod, err := p.Resolver.ResolvePod(ctx, jobRID)
	if err != nil {
		return nil, nil, err
	}
	if namespace == "" || pod == "" {
		return nil, nil, ErrNoPod
	}

	streamCtx, cancelStream := context.WithCancel(ctx)
	body, err := p.Client.StreamPodLogs(streamCtx, namespace, pod, dispatch.PodLogOptions{
		Follow:     true,
		TailLines:  p.TailLines,
		Container:  p.Container,
		Timestamps: p.Timestamps,
	})
	if err != nil {
		cancelStream()
		return nil, nil, fmt.Errorf("sparklogs: stream pod logs %s/%s: %w", namespace, pod, err)
	}

	out := make(chan livellogs.LogEntry, 32)
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			cancelStream()
			_ = body.Close()
		})
	}

	go func() {
		defer close(out)
		defer cancel()
		scanner := bufio.NewScanner(body)
		// kube driver logs can be long; bump the line buffer past the
		// 64KiB default so a stray exception trace doesn't get split.
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		var seq int64
		for scanner.Scan() {
			line := scanner.Text()
			if strings.TrimSpace(line) == "" {
				continue
			}
			seq++
			entry := ParseLine(jobRID, seq, line)
			select {
			case out <- entry:
			case <-streamCtx.Done():
				return
			}
		}
		if err := scanner.Err(); err != nil && !errors.Is(err, context.Canceled) {
			if p.Logger != nil {
				p.Logger.WarnContext(ctx, "sparklogs stream ended with error",
					slog.String("job_rid", jobRID),
					slog.String("namespace", namespace),
					slog.String("pod", pod),
					slog.String("error", err.Error()))
			}
		}
	}()
	return out, cancel, nil
}

// ParseLine wraps a raw pod-log line into a LogEntry. The kube
// timestamps prefix (`2026-05-24T12:00:00.000000000Z `) is stripped
// when present and used as the entry timestamp; otherwise the local
// clock is used. The level is INFO unless the line matches a known
// `[LEVEL]` or `LEVEL:` prefix.
func ParseLine(jobRID string, sequence int64, line string) livellogs.LogEntry {
	ts := time.Now().UTC()
	rest := line
	if maybe, parsed, ok := splitTimestamp(line); ok {
		ts = parsed
		rest = maybe
	}
	level := livellogs.LogInfo
	if detected, stripped, ok := detectLevel(rest); ok {
		level = detected
		rest = stripped
	}
	return livellogs.LogEntry{
		Sequence: sequence,
		JobRID:   jobRID,
		TS:       ts,
		Level:    level,
		Message:  rest,
	}
}

// splitTimestamp peels the RFC3339-nanos prefix that kube emits when
// `timestamps=true` is passed. Returns (rest, ts, true) on a match.
func splitTimestamp(line string) (string, time.Time, bool) {
	idx := strings.IndexByte(line, ' ')
	if idx <= 0 || idx > 40 {
		return line, time.Time{}, false
	}
	if ts, err := time.Parse(time.RFC3339Nano, line[:idx]); err == nil {
		return line[idx+1:], ts.UTC(), true
	}
	if ts, err := time.Parse(time.RFC3339, line[:idx]); err == nil {
		return line[idx+1:], ts.UTC(), true
	}
	return line, time.Time{}, false
}

// detectLevel pulls a leading `[LEVEL]` or `LEVEL:` token from the
// front of the line and maps it to the canonical LogLevel. Returns
// (level, rest, true) on a match.
func detectLevel(line string) (livellogs.LogLevel, string, bool) {
	if strings.HasPrefix(line, "[") {
		if end := strings.IndexByte(line, ']'); end > 0 && end < 12 {
			token := line[1:end]
			if level, ok := livellogs.ParseLogLevel(token); ok {
				return level, strings.TrimSpace(line[end+1:]), true
			}
		}
	}
	for _, prefix := range []string{"TRACE: ", "DEBUG: ", "INFO: ", "WARN: ", "WARNING: ", "ERROR: ", "FATAL: "} {
		if strings.HasPrefix(line, prefix) {
			level, _ := livellogs.ParseLogLevel(strings.TrimSuffix(prefix, ": "))
			return level, line[len(prefix):], true
		}
	}
	return livellogs.LogInfo, line, false
}
