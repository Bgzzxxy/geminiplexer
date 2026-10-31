package gemini

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const DefaultRotateEndpoint = "https://accounts.google.com/RotateCookies"

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

	bodyStr := `[000,"-0000000000000000000"]`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(bodyStr))
	if err != nil {
		return fmt.Errorf("create rotation request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Cookie", cookieStr)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("execute rotation request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("cookie rotation failed with HTTP %d", resp.StatusCode)
	}

	found := false
	for _, c := range resp.Cookies() {
		if c.Name == "__Secure-1PSIDTS" {
			s.mu.Lock()
			s.Secure1PSIDTS = c.Value
			s.LastRotated = time.Now()
			s.mu.Unlock()
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("no __Secure-1PSIDTS cookie returned in rotation response")
	}

	return nil
}
