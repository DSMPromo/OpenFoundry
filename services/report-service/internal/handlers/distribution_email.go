package handlers

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// SMTPConfig is the report-service SMTP relay settings. An empty Host
// disables email delivery — the Distributor records every email
// recipient as "skipped — smtp not configured" until the operator
// wires real values.
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	// From is the envelope sender + From header on every report email.
	// Required when Host is set; falls back to Username when empty.
	From string
}

func (c SMTPConfig) configured() bool { return c.Host != "" }

func (c SMTPConfig) fromAddress() string {
	if c.From != "" {
		return c.From
	}
	return c.Username
}

func (c SMTPConfig) addr() string {
	port := c.Port
	if port == 0 {
		port = 587
	}
	return c.Host + ":" + strconv.Itoa(port)
}

// errSMTPNotConfigured is returned when an email recipient is asked to
// deliver but the SMTP relay has no Host configured. The Distributor
// translates it into a recipient-level "skipped" result, not a failure.
var errSMTPNotConfigured = errors.New("smtp not configured")

// sendEmail composes a MIME message carrying the rendered report
// artifact as an attachment and hands it to the configured SMTP relay.
// A nil error means the relay accepted the message; the recipient may
// still bounce out-of-band.
func (d *Distributor) sendEmail(_ context.Context, r DistributionRecipient, e ReportExecution, artifact []byte) error {
	if !d.smtp.configured() {
		return errSMTPNotConfigured
	}
	if strings.TrimSpace(r.Target) == "" {
		return fmt.Errorf("email recipient target is empty")
	}
	from := d.smtp.fromAddress()
	if from == "" {
		return fmt.Errorf("smtp From address is empty")
	}
	subject := fmt.Sprintf("Report %q ready", e.ReportName)
	msg := composeEmail(emailMessage{
		From:           from,
		To:             r.Target,
		Subject:        subject,
		Body:           chatMessage(e),
		Attachment:     artifact,
		AttachmentName: e.Artifact.FileName,
		AttachmentMime: e.Artifact.MimeType,
	})
	var auth smtp.Auth
	if d.smtp.Username != "" {
		auth = smtp.PlainAuth("", d.smtp.Username, d.smtp.Password, d.smtp.Host)
	}
	return smtp.SendMail(d.smtp.addr(), auth, from, []string{r.Target}, msg)
}

// emailMessage carries the pieces composeEmail assembles.
type emailMessage struct {
	From, To, Subject string
	Body              string
	Attachment        []byte
	AttachmentName    string
	AttachmentMime    string
}

// composeEmail builds a multipart/mixed RFC 2045 message: a text/plain
// part for the body and a base64-encoded attachment for the artifact.
// It is exported as an exported-by-tests helper to keep MIME shape
// verifiable without an SMTP relay.
func composeEmail(m emailMessage) []byte {
	const boundary = "openfoundry-report-boundary"
	mime := m.AttachmentMime
	if mime == "" {
		mime = "application/octet-stream"
	}
	name := m.AttachmentName
	if name == "" {
		name = "report"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", m.From)
	fmt.Fprintf(&b, "To: %s\r\n", m.To)
	fmt.Fprintf(&b, "Subject: %s\r\n", m.Subject)
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().UTC().Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\n")
	fmt.Fprintf(&b, "Content-Type: multipart/mixed; boundary=%q\r\n", boundary)
	b.WriteString("\r\n")

	fmt.Fprintf(&b, "--%s\r\n", boundary)
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 7bit\r\n\r\n")
	b.WriteString(m.Body)
	b.WriteString("\r\n")

	fmt.Fprintf(&b, "--%s\r\n", boundary)
	fmt.Fprintf(&b, "Content-Type: %s; name=%q\r\n", mime, name)
	b.WriteString("Content-Transfer-Encoding: base64\r\n")
	fmt.Fprintf(&b, "Content-Disposition: attachment; filename=%q\r\n\r\n", name)
	encoded := base64.StdEncoding.EncodeToString(m.Attachment)
	// Wrap to 76-character lines (RFC 2045 §6.8).
	for i := 0; i < len(encoded); i += 76 {
		end := i + 76
		if end > len(encoded) {
			end = len(encoded)
		}
		b.WriteString(encoded[i:end])
		b.WriteString("\r\n")
	}
	fmt.Fprintf(&b, "--%s--\r\n", boundary)
	return []byte(b.String())
}
