package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	telemetryRequestTimeout = 3 * time.Second
	telemetryCloseTimeout   = 1 * time.Second
)

// telemetryEndpointURL is deliberately a public Worker URL. It is only an
// append-only activity signal, never a credential or part of access control.
// A release ID is already embedded in each executable for the online policy.
var telemetryEndpointURL = "https://katools-telemetry.tabsfauzihananta.workers.dev"

var telemetrySequence atomic.Uint64

type telemetryPayload struct {
	ReleaseID  string `json:"releaseID"`
	InstanceID string `json:"instanceID"`
	EventType  string `json:"eventType"`
	StopReason string `json:"stopReason,omitempty"`
}

// TelemetryReporter records one open and one best-effort close event per app
// process. It does not run a polling loop or retain an HTTP connection.
type TelemetryReporter struct {
	endpoint   string
	releaseID  string
	instanceID string
	http       *http.Client
	closeOnce  sync.Once
}

func NewTelemetryReporter(release string) *TelemetryReporter {
	sequence := telemetrySequence.Add(1)
	return &TelemetryReporter{
		endpoint:   strings.TrimRight(strings.TrimSpace(telemetryEndpointURL), "/"),
		releaseID:  strings.TrimSpace(release),
		instanceID: fmt.Sprintf("%x-%x", time.Now().UnixNano(), sequence),
		http: &http.Client{
			Timeout: telemetryRequestTimeout,
			Transport: &http.Transport{
				DisableKeepAlives: true,
			},
		},
	}
}

// Open is asynchronous, so a slow or unavailable network can never delay the
// desktop UI from appearing.
func (r *TelemetryReporter) Open() {
	r.sendAsync("open", "")
}

// RuntimeStarted and RuntimeStopped report bot state transitions only. They
// are not a heartbeat and make exactly one short-lived request per transition.
func (r *TelemetryReporter) RuntimeStarted() {
	r.sendAsync("running", "")
}

func (r *TelemetryReporter) RuntimeStopped(reason string) {
	r.sendAsync("stopped", reason)
}

// Close gives the last event at most one second during normal app shutdown.
// A crash, forced termination, or lost network can still prevent this event.
func (r *TelemetryReporter) Close() {
	if r == nil || r.endpoint == "" || r.releaseID == "" {
		return
	}
	r.closeOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), telemetryCloseTimeout)
		defer cancel()
		_ = r.send(ctx, "close", "")
	})
}

func (r *TelemetryReporter) sendAsync(eventType, stopReason string) {
	if r == nil || r.endpoint == "" || r.releaseID == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), telemetryRequestTimeout)
		defer cancel()
		_ = r.send(ctx, eventType, stopReason)
	}()
}

func (r *TelemetryReporter) send(ctx context.Context, eventType, stopReason string) error {
	if eventType != "open" && eventType != "close" && eventType != "running" && eventType != "stopped" {
		return fmt.Errorf("unsupported telemetry event %q", eventType)
	}
	if eventType != "stopped" {
		stopReason = ""
	}
	payload, err := json.Marshal(telemetryPayload{
		ReleaseID:  r.releaseID,
		InstanceID: r.instanceID,
		EventType:  eventType,
		StopReason: strings.TrimSpace(stopReason),
	})
	if err != nil {
		return fmt.Errorf("encode telemetry event: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint+"/event", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create telemetry request: %w", err)
	}
	req.Close = true
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.http.Do(req)
	if err != nil {
		return fmt.Errorf("send telemetry event: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("send telemetry event: server returned %s", resp.Status)
	}
	return nil
}
