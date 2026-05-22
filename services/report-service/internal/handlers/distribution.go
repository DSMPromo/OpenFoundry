package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/openfoundry/openfoundry-go/services/report-service/internal/generator"
)

// Distributor delivers a generated report artifact to a definition's
// recipients. The HTTP-based channels (webhook, slack, teams) are sent
// directly; email and object-store (s3) delivery require external
// infrastructure and are recorded as skipped until that is configured.
type Distributor struct {
	client *http.Client
}

// NewDistributor builds a Distributor with a bounded HTTP client.
func NewDistributor() *Distributor {
	return &Distributor{client: &http.Client{Timeout: 15 * time.Second}}
}

// Deliver sends the artifact to every recipient and returns one result
// per recipient. A delivery failure is recorded on its result and never
// returned as an error — report generation succeeds regardless.
func (d *Distributor) Deliver(ctx context.Context, e ReportExecution, recipients []DistributionRecipient, artifact []byte) []DistributionResult {
	out := make([]DistributionResult, 0, len(recipients))
	for _, r := range recipients {
		out = append(out, d.deliverOne(ctx, r, e, artifact))
	}
	return out
}

func (d *Distributor) deliverOne(ctx context.Context, r DistributionRecipient, e ReportExecution, artifact []byte) DistributionResult {
	res := DistributionResult{
		Channel:     r.Channel,
		Target:      r.Target,
		DeliveredAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	var err error
	switch r.Channel {
	case "webhook":
		err = d.postJSON(ctx, r.Target, webhookPayload(e, len(artifact)))
	case "slack", "teams":
		err = d.postJSON(ctx, r.Target, map[string]string{"text": chatMessage(e)})
	case "email":
		res.Status = "skipped"
		res.Detail = "email delivery requires an SMTP relay to be configured"
		return res
	case "s3":
		res.Status = "skipped"
		res.Detail = "object-store delivery requires bucket credentials to be configured"
		return res
	default:
		res.Status = "skipped"
		res.Detail = "unsupported delivery channel: " + string(r.Channel)
		return res
	}
	if err != nil {
		res.Status = "failed"
		res.Detail = err.Error()
		return res
	}
	res.Status = "delivered"
	res.Detail = "delivered via " + string(r.Channel)
	return res
}

// postJSON POSTs body as JSON to url; any 2xx response is success.
func (d *Distributor) postJSON(ctx context.Context, url string, body any) error {
	if url == "" {
		return fmt.Errorf("recipient target URL is empty")
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("delivery endpoint returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// chatMessage is the short notification posted to Slack / Teams.
func chatMessage(e ReportExecution) string {
	return fmt.Sprintf("Report %q generated (%s) — execution %s, %d section(s).",
		e.ReportName, e.GeneratorKind, e.ID, e.Metrics.SectionCount)
}

// webhookPayload is the JSON body POSTed to a generic webhook recipient.
func webhookPayload(e ReportExecution, artifactSize int) map[string]any {
	return map[string]any{
		"event":          "report.generated",
		"execution_id":   e.ID,
		"report_id":      e.ReportID,
		"report_name":    e.ReportName,
		"generator_kind": e.GeneratorKind,
		"generated_at":   e.GeneratedAt,
		"artifact": map[string]any{
			"file_name":  e.Artifact.FileName,
			"mime_type":  e.Artifact.MimeType,
			"size_bytes": artifactSize,
			"checksum":   e.Artifact.Checksum,
			"url":        e.Artifact.StorageURL,
		},
	}
}

// AttachDistribution renders the execution artifact and delivers it to
// recipients, replacing e.Distributions with the real per-recipient
// results. A nil Distributor or empty recipient list is a no-op, so the
// distributions buildExecution recorded are left intact.
func AttachDistribution(ctx context.Context, dist *Distributor, e *ReportExecution, recipients []DistributionRecipient) {
	if dist == nil || len(recipients) == 0 {
		return
	}
	artifact, err := generator.Render(string(e.GeneratorKind), reportDocument(*e))
	if err != nil {
		return
	}
	e.Distributions = dist.Deliver(ctx, *e, recipients, artifact)
}
