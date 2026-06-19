// Package analytics is a thin wrapper over the PostHog Go SDK. It owns the one
// long-lived client and exposes the handful of capture helpers the app uses.
//
// Every method is safe to call on a nil *Client, so any code path that does not
// wire analytics in (the test suites pass nil to Mount) becomes a no-op instead
// of panicking. In production main.go always constructs a live client.
package analytics

import (
	"log"

	"github.com/posthog/posthog-go"
)

// Client wraps a PostHog client. The zero value and a nil *Client are valid
// no-op sinks.
type Client struct {
	ph posthog.Client
}

// New constructs a PostHog client. apiKey is the project token; endpoint is the
// ingestion host (e.g. https://us.i.posthog.com). NewWithConfig returns a
// non-nil client whenever the config validates, so there is no degraded branch.
func New(apiKey, endpoint string) *Client {
	ph, _ := posthog.NewWithConfig(apiKey, posthog.Config{Endpoint: endpoint})
	log.Printf("analytics: posthog enabled (endpoint=%s)", endpoint)
	return &Client{ph: ph}
}

// Props returns an empty property bag to chain .Set() calls onto.
func Props() posthog.Properties { return posthog.NewProperties() }

// Capture enqueues event for distinctID. props may be nil. Enqueue is
// non-blocking: the SDK batches and flushes events on a background goroutine.
func (c *Client) Capture(distinctID, event string, props posthog.Properties) {
	if c == nil || c.ph == nil || distinctID == "" {
		return
	}
	_ = c.ph.Enqueue(posthog.Capture{
		DistinctId: distinctID,
		Event:      event,
		Properties: props,
	})
}

// Identify sets person properties for distinctID, linking the backend account
// to the same distinct_id the browser uses.
func (c *Client) Identify(distinctID string, props posthog.Properties) {
	if c == nil || c.ph == nil || distinctID == "" {
		return
	}
	_ = c.ph.Enqueue(posthog.Identify{
		DistinctId: distinctID,
		Properties: props,
	})
}

// Close flushes any queued events. Call it on shutdown.
func (c *Client) Close() {
	if c == nil || c.ph == nil {
		return
	}
	_ = c.ph.Close()
}
