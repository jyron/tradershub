package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// Mailer sends transactional email through the Resend HTTP API. An empty API
// key turns every Send into a logged no-op, so local dev and tests never need
// mail credentials.
type Mailer struct {
	APIKey   string
	From     string
	ReplyTo  string
	Endpoint string // overridable in tests; default https://api.resend.com/emails
	Client   *http.Client
}

func NewMailer(apiKey, from, replyTo string) *Mailer {
	return &Mailer{
		APIKey:   apiKey,
		From:     from,
		ReplyTo:  replyTo,
		Endpoint: "https://api.resend.com/emails",
		Client:   &http.Client{Timeout: 15 * time.Second},
	}
}

// Enabled reports whether sends will actually go out.
func (m *Mailer) Enabled() bool { return m != nil && m.APIKey != "" }

// Send delivers one email. Callers are expected to invoke it from a goroutine;
// it blocks on the HTTP round trip and returns any transport/API error.
func (m *Mailer) Send(to, subject, text, html string) error {
	if !m.Enabled() {
		log.Printf("mailer disabled (no RESEND_API_KEY): skipped %q to %s", subject, to)
		return nil
	}
	body, err := json.Marshal(map[string]any{
		"from":     m.From,
		"to":       []string{to},
		"reply_to": m.ReplyTo,
		"subject":  subject,
		"text":     text,
		"html":     html,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, m.Endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+m.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("resend: %s: %s", resp.Status, bytes.TrimSpace(msg))
	}
	return nil
}
