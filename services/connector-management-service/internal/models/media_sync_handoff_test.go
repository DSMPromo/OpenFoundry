package models

import "testing"

func TestConnectorSupportsMediaSync(t *testing.T) {
	cases := []struct {
		connector string
		want      bool
	}{
		{"s3", true},
		{"S3", true},
		{"onelake", true},
		{"abfs", true},
		{"postgresql", false},
		{"http", false},
		{"", false},
	}
	for _, tc := range cases {
		got := ConnectorSupportsMediaSync(tc.connector)
		if got != tc.want {
			t.Fatalf("ConnectorSupportsMediaSync(%q) = %v, want %v", tc.connector, got, tc.want)
		}
	}
}

func TestClassifyMediaSetSyncRunStatus(t *testing.T) {
	if status := ClassifyMediaSetSyncRunStatus(nil, nil); status != MediaSetSyncRunStatusFailed {
		t.Fatalf("nil report should be failed, got %s", status)
	}

	clean := &MediaSetSyncExecutionReport{Stats: SyncStats{Accepted: 3}}
	if status := ClassifyMediaSetSyncRunStatus(clean, nil); status != MediaSetSyncRunStatusSucceeded {
		t.Fatalf("clean report should succeed, got %s", status)
	}

	mixed := &MediaSetSyncExecutionReport{Stats: SyncStats{Accepted: 3, SchemaMismatched: 1}}
	if status := ClassifyMediaSetSyncRunStatus(mixed, nil); status != MediaSetSyncRunStatusPartiallySucceeded {
		t.Fatalf("schema mismatch should be partial, got %s", status)
	}

	dispatchErr := &MediaSetSyncExecutionReport{Stats: SyncStats{Accepted: 1}, DispatchErrors: 2}
	if status := ClassifyMediaSetSyncRunStatus(dispatchErr, nil); status != MediaSetSyncRunStatusPartiallySucceeded {
		t.Fatalf("dispatch errors should be partial, got %s", status)
	}

	if status := ClassifyMediaSetSyncRunStatus(clean, errSample("boom")); status != MediaSetSyncRunStatusFailed {
		t.Fatalf("runtime error must mark failed, got %s", status)
	}
}

type errSample string

func (e errSample) Error() string { return string(e) }

func TestComputeMediaSetSyncBytesAccepted_FiltersBySizeAndMIME(t *testing.T) {
	limit := uint64(1024 * 1024)
	filters := MediaSetSyncFilters{
		FileSizeLimit:        &limit,
		ExcludeAlreadySynced: true,
	}
	request := &RunMediaSetSyncRequest{
		SourceFiles: []SourceFile{
			{Path: "a.png", SizeBytes: 200, MimeType: "image/png"},                // accepted
			{Path: "b.png", SizeBytes: 800, MimeType: "image/png"},                // accepted
			{Path: "huge.png", SizeBytes: 5 * 1024 * 1024, MimeType: "image/png"}, // exceeds size limit
			{Path: "skipped.png", SizeBytes: 100, MimeType: "image/png"},          // already synced
			{Path: "doc.pdf", SizeBytes: 50, MimeType: "application/pdf"},         // mime not allowed
		},
		AlreadySynced:    []string{"skipped.png"},
		AllowedMIMETypes: []string{"image/png"},
	}
	report := &MediaSetSyncExecutionReport{Stats: SyncStats{Accepted: 2}}
	got := ComputeMediaSetSyncBytesAccepted(report, request, filters)
	want := uint64(1000)
	if got != want {
		t.Fatalf("bytes accepted: got %d, want %d", got, want)
	}
}

func TestCollectSelectedPaths_DedupesAndTrims(t *testing.T) {
	request := &RunMediaSetSyncRequest{
		SourceFiles: []SourceFile{
			{Path: "  a.png  "},
			{Path: "b.png"},
			{Path: "a.png"},
			{Path: ""},
		},
	}
	got := CollectSelectedPaths(request)
	want := []string{"a.png", "b.png"}
	if len(got) != len(want) {
		t.Fatalf("paths len: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("paths[%d]: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestDefaultMediaSetSyncHandoffDelegation(t *testing.T) {
	delegation := DefaultMediaSetSyncHandoffDelegation()
	if delegation.Schema == "" || delegation.Conversion == "" || delegation.Transformations == "" {
		t.Fatalf("delegation strings must be populated: %+v", delegation)
	}
	if delegation.TransactionPolicy == "" || delegation.MediaReference == "" {
		t.Fatalf("delegation strings must be populated: %+v", delegation)
	}
}
