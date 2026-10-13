package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTelemetryReporterSendsEvent(t *testing.T) {
	var got telemetryPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/event" {
			t.Fatalf("request = %s %s, want POST /event", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	reporter := &TelemetryReporter{
		endpoint:   server.URL,
		releaseID:  "test-release",
		instanceID: "test-instance",
		http:       server.Client(),
	}
	if err := reporter.send(context.Background(), "open", ""); err != nil {
		t.Fatalf("send: %v", err)
	}
	if got.ReleaseID != "test-release" || got.InstanceID != "test-instance" || got.EventType != "open" {
		t.Fatalf("payload = %#v", got)
	}
}

func TestTelemetryReporterRejectsUnsupportedEvent(t *testing.T) {
	reporter := NewTelemetryReporter("test-release")
	if err := reporter.send(context.Background(), "heartbeat", ""); err == nil {
		t.Fatal("expected unsupported event to fail")
	}
}

func TestTelemetryReporterSendsStopReason(t *testing.T) {
	var got telemetryPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	reporter := &TelemetryReporter{
		endpoint:   server.URL,
		releaseID:  "test-release",
		instanceID: "test-instance",
		http:       server.Client(),
	}
	if err := reporter.send(context.Background(), "stopped", "dc_detected"); err != nil {
		t.Fatalf("send: %v", err)
	}
	if got.EventType != "stopped" || got.StopReason != "dc_detected" {
		t.Fatalf("payload = %#v", got)
	}
}
