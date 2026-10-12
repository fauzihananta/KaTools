package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	defaultReleaseID          = "2026FreeVersion"
	defaultAccessRefresh      = 24 * time.Hour
	defaultAccessOfflineGrace = 6 * time.Hour
	accessRetryDelay          = 30 * time.Minute
	accessRequestTimeout      = 10 * time.Second
)

// releaseID is embedded in the executable. Free builds use the default above;
// build a premium executable with, for example:
//
//	go build -ldflags "-X main.releaseID=6d39619b-7c97-4a22-9789-d0c18dec9eac"
//
// Removing that ID from katools_access.json revokes that exact executable on
// its next policy check. A replacement premium build must receive a new ID.
var releaseID = defaultReleaseID

// Keep this file in the GitHub repository, not in the distributed KaTools
// folder. It contains no secret: the allow-list is deliberately public.
var accessManifestURL = "https://raw.githubusercontent.com/fauzihananta/KaTools/main/katools_access.json"

type accessManifest struct {
	RefreshEveryHours int      `json:"refreshEveryHours"`
	OfflineGraceHours int      `json:"offlineGraceHours"`
	AllowedReleaseIDs []string `json:"allowedReleaseIDs"`
}

type AccessStatus struct {
	Allowed     bool      `json:"allowed"`
	Checking    bool      `json:"checking"`
	Message     string    `json:"message"`
	ReleaseID   string    `json:"releaseID"`
	CheckedAt   time.Time `json:"checkedAt,omitempty"`
	NextCheckAt time.Time `json:"nextCheckAt,omitempty"`
	ExpiresAt   time.Time `json:"expiresAt,omitempty"`
}

// AccessController owns the remote allow-list. It never keeps a network
// connection open: every request has Connection: close and its body is closed
// before the next scheduled check.
type AccessController struct {
	mu        sync.RWMutex
	status    AccessStatus
	http      *http.Client
	onRevoked func()
}

func NewAccessController() *AccessController {
	return &AccessController{
		status: AccessStatus{
			Message:   "Checking online access...",
			ReleaseID: releaseID,
		},
		http: &http.Client{Timeout: accessRequestTimeout},
	}
}

func (c *AccessController) SetOnRevoked(fn func()) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.onRevoked = fn
	c.mu.Unlock()
}

func (c *AccessController) Status() AccessStatus {
	if c == nil {
		return AccessStatus{Allowed: true, Message: "Access control is not configured."}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status
}

func (c *AccessController) Allowed() bool {
	return c != nil && c.Status().Allowed
}

// Start performs the initial check asynchronously so a slow connection never
// freezes the desktop UI, then repeats it after the policy-selected interval.
func (c *AccessController) Start(ctx context.Context) {
	if c == nil {
		return
	}
	go c.run(ctx)
}

func (c *AccessController) run(ctx context.Context) {
	delay := time.Duration(0)
	for {
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}

		next, err := c.CheckNow(ctx)
		if err != nil {
			delay = c.delayAfterFailedCheck()
			continue
		}
		delay = next
	}
}

// CheckNow fetches and applies one policy. The returned duration is when the
// next ordinary check is due. It is public to make the behaviour testable
// without keeping a connection open.
func (c *AccessController) CheckNow(ctx context.Context) (time.Duration, error) {
	if c == nil {
		return defaultAccessRefresh, nil
	}
	c.setChecking(true)
	defer c.setChecking(false)

	requestCtx, cancel := context.WithTimeout(ctx, accessRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, accessManifestURL, nil)
	if err != nil {
		return 0, c.recordFailedCheck(fmt.Errorf("create access request: %w", err))
	}
	// No persistent GitHub connection is needed for a once-a-day policy read.
	req.Close = true
	req.Header.Set("Cache-Control", "no-cache")

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, c.recordFailedCheck(fmt.Errorf("read online access: %w", err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, c.recordFailedCheck(fmt.Errorf("read online access: server returned %s", resp.Status))
	}

	var manifest accessManifest
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 128*1024))
	if err := decoder.Decode(&manifest); err != nil {
		return 0, c.recordFailedCheck(fmt.Errorf("read online access: invalid JSON: %w", err))
	}
	refresh, grace, err := validateAccessManifest(manifest)
	if err != nil {
		return 0, c.recordFailedCheck(err)
	}

	allowed := false
	for _, id := range manifest.AllowedReleaseIDs {
		if strings.TrimSpace(id) == releaseID {
			allowed = true
			break
		}
	}
	c.applyManifest(allowed, refresh, grace)
	return refresh, nil
}

func validateAccessManifest(manifest accessManifest) (refresh, grace time.Duration, err error) {
	refreshHours := manifest.RefreshEveryHours
	if refreshHours == 0 {
		refreshHours = int(defaultAccessRefresh / time.Hour)
	}
	graceHours := manifest.OfflineGraceHours
	if graceHours == 0 {
		graceHours = int(defaultAccessOfflineGrace / time.Hour)
	}
	if refreshHours < 1 || refreshHours > 168 {
		return 0, 0, fmt.Errorf("read online access: refreshEveryHours must be between 1 and 168")
	}
	if graceHours < 0 || graceHours > 72 {
		return 0, 0, fmt.Errorf("read online access: offlineGraceHours must be between 0 and 72")
	}
	if len(manifest.AllowedReleaseIDs) == 0 {
		return 0, 0, fmt.Errorf("read online access: allowedReleaseIDs is empty")
	}
	return time.Duration(refreshHours) * time.Hour, time.Duration(graceHours) * time.Hour, nil
}

func (c *AccessController) applyManifest(allowed bool, refresh, grace time.Duration) {
	now := time.Now()
	message := "Online access approved."
	if !allowed {
		message = "This KaTools release is disabled by online access policy."
	}

	c.mu.Lock()
	wasAllowed := c.status.Allowed
	c.status = AccessStatus{
		Allowed:     allowed,
		Message:     message,
		ReleaseID:   releaseID,
		CheckedAt:   now,
		NextCheckAt: now.Add(refresh),
		ExpiresAt:   now.Add(refresh + grace),
	}
	onRevoked := c.onRevoked
	c.mu.Unlock()

	if wasAllowed && !allowed && onRevoked != nil {
		onRevoked()
	}
}

func (c *AccessController) recordFailedCheck(cause error) error {
	now := time.Now()
	c.mu.Lock()
	wasAllowed := c.status.Allowed
	stillValid := wasAllowed && now.Before(c.status.ExpiresAt)
	if stillValid {
		c.status.Message = "Online access recheck failed; previous approval is still valid temporarily."
		c.status.NextCheckAt = now.Add(accessRetryDelay)
	} else {
		c.status.Allowed = false
		c.status.Message = "Online access could not be verified."
		c.status.NextCheckAt = now.Add(accessRetryDelay)
	}
	onRevoked := c.onRevoked
	c.mu.Unlock()

	if wasAllowed && !stillValid && onRevoked != nil {
		onRevoked()
	}
	return cause
}

func (c *AccessController) delayAfterFailedCheck() time.Duration {
	c.mu.RLock()
	next := c.status.NextCheckAt
	c.mu.RUnlock()
	if next.IsZero() {
		return accessRetryDelay
	}
	delay := time.Until(next)
	if delay <= 0 {
		return accessRetryDelay
	}
	return delay
}

func (c *AccessController) setChecking(checking bool) {
	c.mu.Lock()
	c.status.Checking = checking
	c.mu.Unlock()
}
