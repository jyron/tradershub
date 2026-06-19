package main

import (
	"os"
	"strings"

	"github.com/posthog/posthog-go"
)

// Analytics is a thin nil-safe wrapper over the PostHog Go SDK for the MCP
// connector. The project token (phc_…) is a public, client-embeddable key, so
// it ships as a default and is overridable via POSTHOG_API_KEY / POSTHOG_HOST.
type Analytics struct {
	ph posthog.Client
}

// NewAnalytics constructs the PostHog client from the environment, falling back
// to the baked-in defaults. NewWithConfig returns a non-nil client whenever the
// config validates, so there is no degraded branch.
func NewAnalytics() *Analytics {
	apiKey := strings.TrimSpace(os.Getenv("POSTHOG_API_KEY"))
	if apiKey == "" {
		apiKey = "phc_wpKjBjqE88hkyBUPLFoowVgeuE8cnhV5MwmE4eUDszw6"
	}
	endpoint := strings.TrimSpace(os.Getenv("POSTHOG_HOST"))
	if endpoint == "" {
		endpoint = "https://us.i.posthog.com"
	}
	ph, _ := posthog.NewWithConfig(apiKey, posthog.Config{Endpoint: endpoint})
	return &Analytics{ph: ph}
}

func phProps() posthog.Properties { return posthog.NewProperties() }

// Capture enqueues event for distinctID. Safe on a nil receiver (tests).
func (a *Analytics) Capture(distinctID, event string, props posthog.Properties) {
	if a == nil || a.ph == nil || distinctID == "" {
		return
	}
	_ = a.ph.Enqueue(posthog.Capture{
		DistinctId: distinctID,
		Event:      event,
		Properties: props,
	})
}

// Close flushes queued events. Important for the short-lived stdio process,
// where the deferred Close in main is what gets events out before exit.
func (a *Analytics) Close() {
	if a == nil || a.ph == nil {
		return
	}
	_ = a.ph.Close()
}
