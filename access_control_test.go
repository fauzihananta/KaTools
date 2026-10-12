package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAccessControllerAllowsOnlyManifestReleaseID(t *testing.T) {
	oldURL, oldID := accessManifestURL, releaseID
	defer func() {
		accessManifestURL = oldURL
		releaseID = oldID
	}()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"refreshEveryHours":24,"offlineGraceHours":6,"allowedReleaseIDs":["allowed-build"]}`)
	}))
	defer server.Close()
	accessManifestURL = server.URL

	releaseID = "allowed-build"
	controller := NewAccessController()
	next, err := controller.CheckNow(context.Background())
	if err != nil {
		t.Fatalf("allowed CheckNow() error = %v", err)
	}
	if !controller.Allowed() {
		t.Fatalf("allowed ID was rejected: %+v", controller.Status())
	}
	if next != 24*time.Hour {
		t.Fatalf("next check = %s, want 24h", next)
	}

	releaseID = "unknown-build"
	if _, err := controller.CheckNow(context.Background()); err != nil {
		t.Fatalf("denied CheckNow() should still read valid manifest: %v", err)
	}
	if controller.Allowed() {
		t.Fatalf("ID outside allowedReleaseIDs was accepted: %+v", controller.Status())
	}
}

func TestAccessControllerRevocationCallsStopHook(t *testing.T) {
	oldURL, oldID := accessManifestURL, releaseID
	defer func() {
		accessManifestURL = oldURL
		releaseID = oldID
	}()

	allowed := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if allowed {
			fmt.Fprint(w, `{"allowedReleaseIDs":["test-build"]}`)
			return
		}
		fmt.Fprint(w, `{"allowedReleaseIDs":["another-build"]}`)
	}))
	defer server.Close()
	accessManifestURL = server.URL
	releaseID = "test-build"

	controller := NewAccessController()
	revoked := make(chan struct{}, 1)
	controller.SetOnRevoked(func() { revoked <- struct{}{} })
	if _, err := controller.CheckNow(context.Background()); err != nil || !controller.Allowed() {
		t.Fatalf("initial approval failed: err=%v status=%+v", err, controller.Status())
	}

	allowed = false
	if _, err := controller.CheckNow(context.Background()); err != nil {
		t.Fatalf("revocation check failed: %v", err)
	}
	if controller.Allowed() {
		t.Fatalf("revoked ID stayed allowed: %+v", controller.Status())
	}
	select {
	case <-revoked:
	case <-time.After(time.Second):
		t.Fatal("revocation did not call the runtime stop hook")
	}
}

func TestAccessManifestRejectsEmptyAllowList(t *testing.T) {
	if _, _, err := validateAccessManifest(accessManifest{}); err == nil {
		t.Fatal("empty allow-list was accepted")
	}
}

func TestRuntimeStartRejectsDisabledOnlineAccess(t *testing.T) {
	access := NewAccessController()
	runtime := NewRuntimeManager()
	runtime.SetAccessController(access)

	err := runtime.Start(1, WebBotConfig{})
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("Start() error = %v, want online-access rejection", err)
	}
}
