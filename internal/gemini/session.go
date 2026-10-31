package gemini

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"geminiplexer/internal/config"
)

var (
	accessTokenRegex         = regexp.MustCompile(`"SNlM0e":"([^"]+)"`)
	accessTokenFallbackRegex = regexp.MustCompile(`\["SNlM0e","([^"]+)"\]`)
	buildLabelRegex          = regexp.MustCompile(`"bl":"([^"]+)"`)
	sessionIDRegex           = regexp.MustCompile(`"f\.sid":"([^"]+)"`)
)

type AccountSession struct {
	mu                     sync.RWMutex
	rotateMu               sync.Mutex
	ID                     string
	AuthUser               string
	RawCookies             string
	Secure1PSID            string
	Secure1PSIDTS          string
	Token                  string
	BuildLabel             string
	SessionID              string
	Client                 *http.Client
	EndpointOverride       string
	RotateEndpointOverride string
	BatchExecOverride      string
	GenerateOverride       string
	CachedModels           []GeminiModel
	LastUsed               time.Time
	LastRotated            time.Time
	CooldownUntil          time.Time
	IsHealthy              bool
}

func parseCookieField(rawCookies, key string) string {
	parts := strings.Split(rawCookies, ";")
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if strings.HasPrefix(trimmed, key+"=") {
			return strings.TrimPrefix(trimmed, key+"=")
		}
	}
	return ""
}

func NewAccountSession(cfg config.AccountConfig) *AccountSession {
	psid := parseCookieField(cfg.Cookies, "__Secure-1PSID")
	psidts := parseCookieField(cfg.Cookies, "__Secure-1PSIDTS")

	return &AccountSession{
		ID:            cfg.ID,
		AuthUser:      cfg.AuthUser,
		RawCookies:    cfg.Cookies,
		Secure1PSID:   psid,
		Secure1PSIDTS: psidts,
		Client: &http.Client{
			Timeout: 5 * time.Minute,
		},
		IsHealthy: true,
	}
}

func (s *AccountSession) SetEndpointOverride(u string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.EndpointOverride = u
}

func (s *AccountSession) UpdateCookies(rawCookies string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.RawCookies = rawCookies
	s.Secure1PSID = parseCookieField(rawCookies, "__Secure-1PSID")
	s.Secure1PSIDTS = parseCookieField(rawCookies, "__Secure-1PSIDTS")
	s.IsHealthy = true
}

func (s *AccountSession) SetRotateEndpointOverride(u string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.RotateEndpointOverride = u
}

func (s *AccountSession) GetToken() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Token
}

func (s *AccountSession) SetToken(t string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Token = t
}

func (s *AccountSession) GetPSIDTS() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Secure1PSIDTS
}

func (s *AccountSession) getFullCookieHeaderLocked() string {
	var parts []string
	if s.Secure1PSID != "" {
		parts = append(parts, fmt.Sprintf("__Secure-1PSID=%s", s.Secure1PSID))
	}
	if s.Secure1PSIDTS != "" {
		parts = append(parts, fmt.Sprintf("__Secure-1PSIDTS=%s", s.Secure1PSIDTS))
	}
	return strings.Join(parts, "; ")
}

func (s *AccountSession) GetFullCookieHeader() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getFullCookieHeaderLocked()
}

func (s *AccountSession) Init(ctx context.Context) error {
	s.mu.Lock()
	endpoint := "https://gemini.google.com/app"
	if s.AuthUser != "" {
		endpoint = fmt.Sprintf("https://gemini.google.com/u/%s/app", s.AuthUser)
	}
	if s.EndpointOverride != "" {
		endpoint = s.EndpointOverride
	}
	cookieHdr := s.getFullCookieHeaderLocked()
	client := s.Client
	s.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("create init request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Cookie", cookieHdr)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("execute init request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("gemini init returned status %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read init body: %w", err)
	}
	body := string(bodyBytes)

	token := ""
	if m := accessTokenRegex.FindStringSubmatch(body); len(m) >= 2 {
		token = m[1]
	} else if m := accessTokenFallbackRegex.FindStringSubmatch(body); len(m) >= 2 {
		token = m[1]
	}

	if token == "" {
		return fmt.Errorf("failed to extract SNlM0e token from response")
	}

	bl := ""
	if m := buildLabelRegex.FindStringSubmatch(body); len(m) >= 2 {
		bl = m[1]
	}

	sessionID := ""
	if m := sessionIDRegex.FindStringSubmatch(body); len(m) >= 2 {
		sessionID = m[1]
	}

	s.mu.Lock()
	s.Token = token
	s.BuildLabel = bl
	s.SessionID = sessionID
	s.IsHealthy = true
	s.mu.Unlock()

	return nil
}
