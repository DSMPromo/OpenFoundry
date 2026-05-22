package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	databus "github.com/openfoundry/openfoundry-go/libs/event-bus-data"
	searchabstraction "github.com/openfoundry/openfoundry-go/libs/search-abstraction"
	"github.com/openfoundry/openfoundry-go/libs/search-abstraction/opensearch"
	"github.com/openfoundry/openfoundry-go/libs/search-abstraction/vespa"
	repos "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
	"github.com/openfoundry/openfoundry-go/services/ontology-indexer/internal/config"
	"github.com/openfoundry/openfoundry-go/services/ontology-indexer/internal/status"
)

type fakeReader struct {
	mu        sync.Mutex
	topics    []string
	messages  []KafkaMessage
	committed []KafkaMessage
	closed    bool
	fetchErr  error
}

func (r *fakeReader) Subscribe(_ context.Context, topics []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.topics = append([]string(nil), topics...)
	return nil
}

func (r *fakeReader) FetchMessage(ctx context.Context) (KafkaMessage, error) {
	r.mu.Lock()
	if len(r.messages) == 0 {
		r.mu.Unlock()
		<-ctx.Done()
		return KafkaMessage{}, ctx.Err()
	}
	msg := r.messages[0]
	r.messages = r.messages[1:]
	err := r.fetchErr
	r.mu.Unlock()
	return msg, err
}

func (r *fakeReader) CommitMessages(_ context.Context, msgs ...KafkaMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.committed = append(r.committed, msgs...)
	return nil
}

func (r *fakeReader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	return nil
}

// committedCount returns len(committed) under the lock, so concurrent
// Eventually pollers can read it safely while RunWith* commits from a
// goroutine.
func (r *fakeReader) committedCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.committed)
}

type fakeBackend struct {
	mu       sync.Mutex
	indexed  []repos.IndexDoc
	deleted  []repos.ObjectId
	err      error
	failures int
}

func (b *fakeBackend) Search(context.Context, repos.SearchQuery, repos.ReadConsistency) (repos.PagedResult[repos.SearchHit], error) {
	return repos.PagedResult[repos.SearchHit]{}, nil
}

func (b *fakeBackend) Index(_ context.Context, doc repos.IndexDoc) error {
	if b.err != nil {
		b.mu.Lock()
		b.failures++
		b.mu.Unlock()
		return b.err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.indexed = append(b.indexed, doc)
	return nil
}

func (b *fakeBackend) Delete(_ context.Context, tenant repos.TenantId, id repos.ObjectId) (bool, error) {
	if b.err != nil {
		b.mu.Lock()
		b.failures++
		b.mu.Unlock()
		return false, b.err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.deleted = append(b.deleted, id)
	return true, nil
}

func (b *fakeBackend) SearchVector(context.Context, repos.VectorQuery, repos.ReadConsistency) ([]repos.SearchHit, error) {
	return nil, repos.ErrVectorSearchUnsupported()
}

func (b *fakeBackend) BulkIndex(ctx context.Context, docs []repos.IndexDoc) (repos.BulkOutcome, error) {
	return repos.DefaultBulkIndex(ctx, b, docs)
}

func TestNewSearchBackendSelectsConfiguredBackend(t *testing.T) {
	cfg := testConfig()
	cfg.SearchEndpoint = "http://search.local"

	cfg.BackendKind = config.BackendVespa
	be, err := NewSearchBackend(cfg)
	require.NoError(t, err)
	assert.IsType(t, &vespa.Backend{}, be)

	cfg.BackendKind = config.BackendOpenSearch
	be, err = NewSearchBackend(cfg)
	require.NoError(t, err)
	assert.IsType(t, &opensearch.Backend{}, be)
}

func TestNewSearchBackendRejectsMissingEndpoint(t *testing.T) {
	cfg := testConfig()
	_, err := NewSearchBackend(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SEARCH_ENDPOINT not set")
}

func TestSearchAuthHeaderPriority(t *testing.T) {
	cfg := testConfig()
	cfg.SearchUsername = "user"
	cfg.SearchPassword = "pass"
	assert.Equal(t, "Basic dXNlcjpwYXNz", searchAuthHeader(cfg))
	cfg.SearchAPIKey = "api-key"
	assert.Equal(t, "ApiKey api-key", searchAuthHeader(cfg))
	cfg.SearchBearerToken = "bearer-token"
	assert.Equal(t, "Bearer bearer-token", searchAuthHeader(cfg))
}

func TestRedactedEndpoint(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "(unset)", redactedEndpoint(""))
	assert.Equal(t, "https://vespa.local:8080", redactedEndpoint("https://vespa.local:8080"))
	assert.Equal(t, "***@vespa.local:8080", redactedEndpoint("https://user:pass@vespa.local:8080"))
}

func TestRunWithReaderHappyPathFakeReaderAndBackend(t *testing.T) {
	cfg := testConfig()
	reader := &fakeReader{messages: []KafkaMessage{{Topic: TopicObjectChangedV1, Offset: 9, Value: mustJSON(t, map[string]any{
		"tenant": "acme", "id": "obj-happy", "type_id": "Aircraft", "version": 1, "payload": map[string]any{"tail_number": "HF-001"},
	})}}}
	backend := &fakeBackend{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- RunWithReader(ctx, cfg, discardLog(), reader, backend) }()
	require.Eventually(t, func() bool { return reader.committedCount() == 1 }, time.Second, 10*time.Millisecond)
	cancel()
	require.NoError(t, <-done)
	assert.Equal(t, SubscribeTopics, reader.topics)
	assert.True(t, reader.closed)
	require.Len(t, backend.indexed, 1)
	assert.Equal(t, repos.ObjectId("obj-happy"), backend.indexed[0].ID)
	assert.Len(t, reader.committed, 1)
}

func TestRunWithReaderObjectUpsertCommitsAfterIndex(t *testing.T) {
	cfg := testConfig()
	reader := &fakeReader{messages: []KafkaMessage{{Topic: TopicObjectChangedV1, Offset: 10, Value: mustJSON(t, map[string]any{
		"tenant": "acme", "id": "obj-1", "type_id": "Aircraft", "version": 7,
		"payload": map[string]any{"tail_number": "EC-123"}, "embedding": []float32{0.1, 0.2},
	})}}}
	backend := &fakeBackend{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- RunWithReader(ctx, cfg, discardLog(), reader, backend) }()
	require.Eventually(t, func() bool { return reader.committedCount() == 1 }, time.Second, 10*time.Millisecond)
	cancel()
	require.NoError(t, <-done)
	require.Len(t, backend.indexed, 1)
	assert.Equal(t, repos.ObjectId("obj-1"), backend.indexed[0].ID)
	assert.Equal(t, repos.TypeId("Aircraft"), backend.indexed[0].TypeID)
	assert.JSONEq(t, `{"tail_number":"EC-123"}`, string(backend.indexed[0].Payload))
	assert.Len(t, reader.committed, 1)
}

func TestRunWithReaderObjectDeleteCommitsAfterDelete(t *testing.T) {
	cfg := testConfig()
	reader := &fakeReader{messages: []KafkaMessage{{Topic: TopicObjectChangedV1, Offset: 11, Value: mustJSON(t, map[string]any{
		"tenant": "acme", "id": "obj-1", "type_id": "Aircraft", "version": 8, "payload": map[string]any{}, "deleted": true,
	})}}}
	backend := &fakeBackend{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- RunWithReader(ctx, cfg, discardLog(), reader, backend) }()
	require.Eventually(t, func() bool { return reader.committedCount() == 1 }, time.Second, 10*time.Millisecond)
	cancel()
	require.NoError(t, <-done)
	assert.Equal(t, []repos.ObjectId{"obj-1"}, backend.deleted)
	assert.Empty(t, backend.indexed)
}

func TestRunWithReaderLinkChangeIndexesLinkDocument(t *testing.T) {
	cfg := testConfig()
	reader := &fakeReader{messages: []KafkaMessage{{Topic: TopicLinkChangedV1, Offset: 12, Value: mustJSON(t, map[string]any{
		"tenant": "acme", "link_type": "owns.asset", "from": "owner-1", "to": "asset-1", "version": 3,
		"payload": map[string]any{"role": "operator"},
	})}}}
	backend := &fakeBackend{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- RunWithReader(ctx, cfg, discardLog(), reader, backend) }()
	require.Eventually(t, func() bool { return reader.committedCount() == 1 }, time.Second, 10*time.Millisecond)
	cancel()
	require.NoError(t, <-done)
	require.Len(t, backend.indexed, 1)
	assert.Equal(t, repos.ObjectId("link:owns.asset:owner-1:asset-1"), backend.indexed[0].ID)
	assert.Equal(t, repos.TypeId("__link_owns_asset"), backend.indexed[0].TypeID)
	assert.JSONEq(t, `{"kind":"ontology_link","link_type":"owns.asset","from":"owner-1","to":"asset-1","payload":{"role":"operator"}}`, string(backend.indexed[0].Payload))
}

func TestRunWithReaderMalformedJSONSkipsAndCommits(t *testing.T) {
	cfg := testConfig()
	reader := &fakeReader{messages: []KafkaMessage{{Topic: TopicObjectChangedV1, Offset: 13, Value: []byte(`{"tenant":`)}}}
	backend := &fakeBackend{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- RunWithReader(ctx, cfg, discardLog(), reader, backend) }()
	require.Eventually(t, func() bool { return reader.committedCount() == 1 }, time.Second, 10*time.Millisecond)
	cancel()
	require.NoError(t, <-done)
	assert.Empty(t, backend.indexed)
	assert.Empty(t, backend.deleted)
}

func TestRunWithReaderBackendErrorDoesNotCommit(t *testing.T) {
	cfg := testConfig()
	boom := errors.New("backend unavailable")
	reader := &fakeReader{messages: []KafkaMessage{{Topic: TopicObjectChangedV1, Offset: 14, Value: mustJSON(t, map[string]any{
		"tenant": "acme", "id": "obj-1", "type_id": "Aircraft", "version": 7, "payload": map[string]any{},
	})}}}
	backend := &fakeBackend{err: boom}
	err := RunWithReader(context.Background(), cfg, discardLog(), reader, backend)
	require.ErrorIs(t, err, boom)
	assert.Empty(t, reader.committed)
}

func TestRunWithReaderSkipsDuplicateAndStaleVersions(t *testing.T) {
	cfg := testConfig()
	reader := &fakeReader{messages: []KafkaMessage{
		{Topic: TopicObjectChangedV1, Offset: 1, Value: mustJSON(t, map[string]any{"tenant": "acme", "id": "obj-1", "type_id": "Aircraft", "version": 7, "payload": map[string]any{"tail_number": "EC-123"}})},
		{Topic: TopicObjectChangedV1, Offset: 2, Value: mustJSON(t, map[string]any{"tenant": "acme", "id": "obj-1", "type_id": "Aircraft", "version": 7, "payload": map[string]any{"tail_number": "EC-123"}})},
		{Topic: TopicObjectChangedV1, Offset: 3, Value: mustJSON(t, map[string]any{"tenant": "acme", "id": "obj-1", "type_id": "Aircraft", "version": 6, "payload": map[string]any{"tail_number": "OLD"}})},
	}}
	backend := &fakeBackend{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- RunWithReader(ctx, cfg, discardLog(), reader, backend) }()
	require.Eventually(t, func() bool { return reader.committedCount() == 3 }, time.Second, 10*time.Millisecond)
	cancel()
	require.NoError(t, <-done)
	require.Len(t, backend.indexed, 1)
	assert.Len(t, reader.committed, 3)
}

func TestRunWithOptionsRetriesThenPublishesDLQAndCommits(t *testing.T) {
	cfg := testConfig()
	cfg.RetryMaxAttempts = 2
	cfg.RetryInitialBackoff = time.Millisecond
	cfg.RetryMaxBackoff = time.Millisecond
	cfg.DLQTopic = TopicDLQ
	reader := &fakeReader{messages: []KafkaMessage{{Topic: TopicObjectChangedV1, Key: []byte("acme/obj-1"), Offset: 15, Value: mustJSON(t, map[string]any{
		"tenant": "acme", "id": "obj-1", "type_id": "Aircraft", "version": 7, "payload": map[string]any{},
	})}}}
	backend := &fakeBackend{err: errors.New("backend unavailable")}
	dlq := &fakeDLQ{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- RunWithOptions(ctx, cfg, discardLog(), reader, backend, dlq) }()
	require.Eventually(t, func() bool { return reader.committedCount() == 1 }, time.Second, 10*time.Millisecond)
	cancel()
	require.NoError(t, <-done)
	assert.Equal(t, 2, backend.failures)
	require.Len(t, dlq.messages, 1)
	assert.Equal(t, cfg.DLQTopic, dlq.messages[0].topic)
	assert.Equal(t, []byte("acme/obj-1"), dlq.messages[0].key)
}

type (
	fakeDLQ    struct{ messages []dlqMessage }
	dlqMessage struct {
		topic        string
		key, payload []byte
	}
)

func (d *fakeDLQ) Publish(_ context.Context, topic string, key, payload []byte, _ *databus.OpenLineageHeaders) error {
	d.messages = append(d.messages, dlqMessage{topic: topic, key: append([]byte(nil), key...), payload: append([]byte(nil), payload...)})
	return nil
}

func TestRunWithReaderStopsOnContextCancel(t *testing.T) {
	cfg := testConfig()
	reader := &fakeReader{}
	backend := &fakeBackend{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- RunWithReader(ctx, cfg, discardLog(), reader, backend) }()
	cancel()
	select {
	case err := <-done:
		assert.NoError(t, err, "RunWithReader should return nil on context.Canceled")
		assert.Equal(t, SubscribeTopics, reader.topics)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("RunWithReader did not return after context cancel")
	}
}

func TestRunWithOptionsAndTrackerRecordsPerTypeStats(t *testing.T) {
	cfg := testConfig()
	eventTime := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	reader := &fakeReader{messages: []KafkaMessage{
		{Topic: TopicObjectChangedV1, Offset: 1, Time: eventTime, Value: mustJSON(t, map[string]any{
			"tenant": "acme", "id": "ac-1", "type_id": "Aircraft", "version": 1, "payload": map[string]any{"tail_number": "N12345"},
		})},
		{Topic: TopicObjectChangedV1, Offset: 2, Time: eventTime, Value: mustJSON(t, map[string]any{
			"tenant": "acme", "id": "ac-2", "type_id": "Aircraft", "version": 1, "payload": map[string]any{"tail_number": "N67890"},
		})},
		{Topic: TopicObjectChangedV1, Offset: 3, Time: eventTime, Value: mustJSON(t, map[string]any{
			"tenant": "acme", "id": "ac-1", "type_id": "Aircraft", "version": 2, "payload": map[string]any{}, "deleted": true,
		})},
	}}
	backend := &fakeBackend{}
	tracker := status.NewTracker()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- RunWithOptionsAndTracker(ctx, cfg, discardLog(), reader, backend, nil, tracker) }()
	require.Eventually(t, func() bool { return reader.committedCount() == 3 }, time.Second, 10*time.Millisecond)
	cancel()
	require.NoError(t, <-done)

	snap, ok := tracker.Snapshot("acme", "Aircraft")
	require.True(t, ok)
	assert.Equal(t, uint64(2), snap.IndexedCount)
	assert.Equal(t, uint64(1), snap.DeletedCount)
	assert.False(t, snap.LastIndexedAt.IsZero())
	assert.Equal(t, eventTime, snap.LastEventTime)
}

func TestTopicsAndConsumerGroup(t *testing.T) {
	t.Parallel()
	// Singular form is the kernel canon — see runtime.go for the
	// post-mortem of the plural-spelling drift (B03 §G1). The
	// object_type topic was added for B03 §G5.
	assert.Equal(t, "ontology.object.changed.v1", TopicObjectChangedV1)
	assert.Equal(t, "ontology.link.changed.v1", TopicLinkChangedV1)
	assert.Equal(t, "ontology.object_type.changed.v1", TopicObjectTypeChangedV1)
	assert.Equal(t, []string{TopicObjectChangedV1, TopicLinkChangedV1, TopicObjectTypeChangedV1}, SubscribeTopics)
	assert.Equal(t, "ontology-indexer", ConsumerGroup)
}

// schemaCapturingBackend embeds fakeBackend and adds the MappingRegistrar
// surface so we can prove the runtime routes object_type events into the
// schemasync handler. Each call is recorded on the type.
type schemaCapturingBackend struct {
	fakeBackend
	regMu      sync.Mutex
	registered []string
	dropped    []string
}

func (b *schemaCapturingBackend) RegisterTypeMapping(_ context.Context, m searchabstraction.TypeMapping) error {
	b.regMu.Lock()
	defer b.regMu.Unlock()
	b.registered = append(b.registered, string(m.TypeID))
	return nil
}

func (b *schemaCapturingBackend) DropTypeMapping(_ context.Context, _ repos.TenantId, typeID repos.TypeId) error {
	b.regMu.Lock()
	defer b.regMu.Unlock()
	b.dropped = append(b.dropped, string(typeID))
	return nil
}

func TestRunWithReaderRoutesObjectTypeEventsToSchemaSync(t *testing.T) {
	cfg := testConfig()
	envelope := mustJSON(t, map[string]any{
		"schema_version": 1,
		"event_type":     "created",
		"aggregate":      "ontology_object_type",
		"aggregate_id":   "Aircraft",
		"after": map[string]any{
			"api_name":    "Aircraft",
			"primary_key": "id",
			"properties": []map[string]any{
				{"name": "tail_number", "property_type": "string", "searchable": true},
			},
		},
	})
	reader := &fakeReader{messages: []KafkaMessage{
		{Topic: TopicObjectTypeChangedV1, Offset: 1, Value: envelope},
	}}
	backend := &schemaCapturingBackend{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- RunWithReader(ctx, cfg, discardLog(), reader, backend) }()
	require.Eventually(t, func() bool { return reader.committedCount() == 1 }, time.Second, 10*time.Millisecond)
	cancel()
	require.NoError(t, <-done)
	assert.Equal(t, []string{"Aircraft"}, backend.registered)
}

func testConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Service.Name = "ontology-indexer"
	cfg.BackendKind = config.BackendVespa
	cfg.ConsumerGroup = ConsumerGroup
	return cfg
}

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}
