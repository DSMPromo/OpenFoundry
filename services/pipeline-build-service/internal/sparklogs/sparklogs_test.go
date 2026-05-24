package sparklogs

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/dispatch"
	livellogs "github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/logs"
)

// stubResolver returns canned (namespace, pod) on every Resolve call,
// or a configured error.
type stubResolver struct {
	namespace string
	pod       string
	err       error
}

func (s *stubResolver) ResolvePod(_ context.Context, _ string) (string, string, error) {
	return s.namespace, s.pod, s.err
}

// stubStreamer hands back whatever ReadCloser is set on it. Captures
// the (namespace, pod, opts) trio for assertion.
type stubStreamer struct {
	body         io.ReadCloser
	err          error
	gotNamespace string
	gotPod       string
	gotOpts      dispatch.PodLogOptions
}

func (s *stubStreamer) StreamPodLogs(_ context.Context, namespace, podName string, opts dispatch.PodLogOptions) (io.ReadCloser, error) {
	s.gotNamespace = namespace
	s.gotPod = podName
	s.gotOpts = opts
	if s.err != nil {
		return nil, s.err
	}
	return s.body, nil
}

func TestPodSubscriberStreamsAndCancels(t *testing.T) {
	body := io.NopCloser(strings.NewReader("first\nsecond\n[ERROR] boom\n"))
	streamer := &stubStreamer{body: body}
	resolver := &stubResolver{namespace: "ns", pod: "build-driver"}
	sub := &PodSubscriber{Client: streamer, Resolver: resolver}

	ch, cancel, err := sub.Subscribe(context.Background(), "ri.job.x")
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()

	collected := make([]livellogs.LogEntry, 0, 3)
	timeout := time.After(2 * time.Second)
	for len(collected) < 3 {
		select {
		case entry, ok := <-ch:
			if !ok {
				goto done
			}
			collected = append(collected, entry)
		case <-timeout:
			t.Fatalf("timed out; got %d entries: %+v", len(collected), collected)
		}
	}
done:
	if len(collected) != 3 {
		t.Fatalf("expected 3 entries, got %d: %+v", len(collected), collected)
	}
	if collected[0].Message != "first" || collected[1].Message != "second" {
		t.Errorf("plain messages: %+v", collected[:2])
	}
	if collected[2].Level != livellogs.LogError || collected[2].Message != "boom" {
		t.Errorf("[ERROR] prefix not parsed: %+v", collected[2])
	}
	if streamer.gotNamespace != "ns" || streamer.gotPod != "build-driver" || !streamer.gotOpts.Follow {
		t.Errorf("streamer called with wrong args: %+v", streamer)
	}
}

func TestPodSubscriberResolverErrorPropagates(t *testing.T) {
	sub := &PodSubscriber{
		Client:   &stubStreamer{},
		Resolver: &stubResolver{err: errors.New("db down")},
	}
	_, _, err := sub.Subscribe(context.Background(), "ri.job.x")
	if err == nil {
		t.Fatal("expected resolver error")
	}
}

func TestPodSubscriberReturnsErrNoPodWhenResolverEmpty(t *testing.T) {
	sub := &PodSubscriber{
		Client:   &stubStreamer{},
		Resolver: &stubResolver{namespace: "", pod: ""},
	}
	_, _, err := sub.Subscribe(context.Background(), "ri.job.x")
	if !errors.Is(err, ErrNoPod) {
		t.Fatalf("expected ErrNoPod, got %v", err)
	}
}

func TestPodSubscriberRequiresConfig(t *testing.T) {
	cases := map[string]*PodSubscriber{
		"nil": nil,
		"no client":   {Resolver: &stubResolver{}},
		"no resolver": {Client: &stubStreamer{}},
	}
	for name, sub := range cases {
		_, _, err := sub.Subscribe(context.Background(), "x")
		if err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestParseLineExtractsTimestampAndLevel(t *testing.T) {
	cases := []struct {
		name  string
		raw   string
		wantL livellogs.LogLevel
		wantM string
		wantTS bool
	}{
		{"plain", "starting executor", livellogs.LogInfo, "starting executor", false},
		{"bracket", "[WARN] slow", livellogs.LogWarn, "slow", false},
		{"colon", "ERROR: boom", livellogs.LogError, "boom", false},
		{"timestamp", "2026-05-24T12:00:00.000000000Z hi", livellogs.LogInfo, "hi", true},
		{"timestamp+level", "2026-05-24T12:00:00Z [ERROR] boom", livellogs.LogError, "boom", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entry := ParseLine("ri.job.x", 1, tc.raw)
			if entry.Level != tc.wantL {
				t.Errorf("level = %q, want %q", entry.Level, tc.wantL)
			}
			if entry.Message != tc.wantM {
				t.Errorf("message = %q, want %q", entry.Message, tc.wantM)
			}
			if entry.JobRID != "ri.job.x" || entry.Sequence != 1 {
				t.Errorf("metadata wrong: %+v", entry)
			}
			if tc.wantTS && entry.TS.Year() != 2026 {
				t.Errorf("timestamp not extracted: %v", entry.TS)
			}
		})
	}
}

func TestParseLineSkipsTimestampWhenMalformed(t *testing.T) {
	entry := ParseLine("rid", 1, "thisIsNotAtTS lineBody")
	// Whole string stays as message; ts becomes "now".
	if entry.Message != "thisIsNotAtTS lineBody" {
		t.Errorf("unexpected message: %q", entry.Message)
	}
}
