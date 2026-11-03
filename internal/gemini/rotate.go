package gemini

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"geminiplexer/internal/transport"
)

const (
	DefaultRotateEndpoint     = "https://accounts.google.com/RotateCookies"
	DefaultRotatePageEndpoint = "https://accounts.google.com/RotateCookiesPage"
)

// RotateCookies performs dual-path cookie rotation:
//  1. Primary path: POST /RotateCookies with [000,"-0000000000000000000"] → refreshes __Secure-1PSIDTS
//  2. Secondary path: GET /RotateCookiesPage?og_pid=658 → POST /RotateCookies [658," "] → refreshes SIDCC
//
// Both paths are attempted. If the primary fails, the error is returned.
// After rotation, SNlM0e is self-healed if stale.
func (s *AccountSession) RotateCookies(ctx context.Context) error {
	s.rotateMu.Lock()
	defer s.rotateMu.Unlock()

	s.mu.Lock()
	endpoint := DefaultRotateEndpoint
	if s.RotateEndpointOverride != "" {
		endpoint = s.RotateEndpointOverride
	}
	cookieStr := s.getFullCookieHeaderLocked()
	client := s.Client
	s.mu.Unlock()

	// === Path 1: Rotate __Secure-1PSIDTS ===
	if err := s.rotatePSIDTS(ctx, client, endpoint, cookieStr); err != nil {
		log.Printf("[Rotate] %s: primary rotation (PSIDTS) failed: %v", s.ID, err)
		return err
	}

	// === Path 2: Rotate SIDCC (best-effort) ===
	// Re-read cookie string after PSIDTS rotation
	s.mu.RLock()
	cookieStr = s.getFullCookieHeaderLocked()
	s.mu.RUnlock()

	if err := s.rotateSIDCC(ctx, client, endpoint, cookieStr); err != nil {
		log.Printf("[Rotate] %s: secondary rotation (SIDCC) failed (non-fatal): %v", s.ID, err)
	}

	s.mu.Lock()
	s.LastRotated = time.Now()
	s.mu.Unlock()

	// Persist rotated/absorbed cookies back to disk so a restart reloads fresh
	// credentials instead of the stale on-disk value.
	s.PersistCookies()

	// === Self-heal SNlM0e if it might be stale ===
	s.mu.RLock()
	token := s.Token
	s.mu.RUnlock()
	if token == "" {
		log.Printf("[Rotate] %s: SNlM0e missing after rotation, triggering self-heal", s.ID)
		s.SelfHeal(ctx)
	}

	return nil
}

// rotatePSIDTS performs the primary cookie rotation.
func (s *AccountSession) rotatePSIDTS(ctx context.Context, client *http.Client, endpoint, cookieStr string) error {
	bodyStr := `[000,"-0000000000000000000"]`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(bodyStr))
	if err != nil {
		return fmt.Errorf("create PSIDTS rotation request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	transport.SetChromeHeaders(req)
	req.Header.Set("Cookie", cookieStr)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("execute PSIDTS rotation: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("PSIDTS rotation HTTP %d", resp.StatusCode)
	}

	// Absorb cookies from response
	s.absorbSetCookies(resp)

	found := false
	for _, c := range resp.Cookies() {
		if c.Name == "__Secure-1PSIDTS" {
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("no __Secure-1PSIDTS cookie returned in rotation response")
	}

	log.Printf("[Rotate] %s: PSIDTS rotated successfully", s.ID)
	return nil
}

// rotateSIDCC performs secondary cookie rotation via RotateCookiesPage.
func (s *AccountSession) rotateSIDCC(ctx context.Context, client *http.Client, endpoint, cookieStr string) error {
	// Step 1: GET /RotateCookiesPage?og_pid=658
	pageReq, err := http.NewRequestWithContext(ctx, http.MethodGet, DefaultRotatePageEndpoint+"?og_pid=658", nil)
	if err != nil {
		return fmt.Errorf("create SIDCC page request: %w", err)
	}
	transport.SetChromeHeaders(pageReq)
	pageReq.Header.Set("Cookie", cookieStr)

	pageResp, err := client.Do(pageReq)
	if err != nil {
		return fmt.Errorf("execute SIDCC page request: %w", err)
	}
	io.Copy(io.Discard, pageResp.Body)
	pageResp.Body.Close()
	s.absorbSetCookies(pageResp)

	// Update cookie string with fresh values
	s.mu.RLock()
	cookieStr = s.getFullCookieHeaderLocked()
	s.mu.RUnlock()

	// Step 2: POST /RotateCookies [658," "]
	bodyStr := `[658," "]`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(bodyStr))
	if err != nil {
		return fmt.Errorf("create SIDCC rotation request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	transport.SetChromeHeaders(req)
	req.Header.Set("Cookie", cookieStr)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("execute SIDCC rotation: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("SIDCC rotation HTTP %d", resp.StatusCode)
	}

	s.absorbSetCookies(resp)
	log.Printf("[Rotate] %s: SIDCC rotated successfully", s.ID)
	return nil
}
