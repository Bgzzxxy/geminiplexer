package gemini

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"geminiplexer/internal/config"
	"geminiplexer/internal/transport"
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
	SIDCC                  string
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
	QuotaResetAt           time.Time
	IsHealthy              bool
	ProxyURL               string
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
	sidcc := parseCookieField(cfg.Cookies, "SIDCC")

	// Create stealth client with Chrome uTLS fingerprint
	client, err := transport.NewStealthClient(cfg.Proxy, 5*time.Minute)
	if err != nil {
		log.Printf("[Session] Warning: failed to create stealth client for %s, falling back to standard: %v", cfg.ID, err)
		client = &http.Client{Timeout: 5 * time.Minute}
	}

	return &AccountSession{
		ID:            cfg.ID,
		AuthUser:      cfg.AuthUser,
		RawCookies:    cfg.Cookies,
		Secure1PSID:   psid,
		Secure1PSIDTS: psidts,
		SIDCC:         sidcc,
		Client:        client,
		IsHealthy:     true,
		ProxyURL:      cfg.Proxy,
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
	s.SIDCC = parseCookieField(rawCookies, "SIDCC")
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
	if s.SIDCC != "" {
		parts = append(parts, fmt.Sprintf("SIDCC=%s", s.SIDCC))
	}
	return strings.Join(parts, "; ")
}

func (s *AccountSession) GetFullCookieHeader() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getFullCookieHeaderLocked()
}

// absorbSetCookies reads Set-Cookie headers from any response and updates session state.
func (s *AccountSession) absorbSetCookies(resp *http.Response) {
	for _, c := range resp.Cookies() {
		switch c.Name {
		case "__Secure-1PSIDTS":
			s.mu.Lock()
			s.Secure1PSIDTS = c.Value
			s.mu.Unlock()
		case "__Secure-1PSID":
			s.mu.Lock()
			s.Secure1PSID = c.Value
			s.mu.Unlock()
		case "SIDCC":
			s.mu.Lock()
			s.SIDCC = c.Value
			s.mu.Unlock()
		}
	}
}

// Init fetches /app to extract SNlM0e token, build label, and session ID.
// Also absorbs any updated cookies from the response.
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
	transport.SetNavigateHeaders(req)
	req.Header.Set("Cookie", cookieHdr)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("execute init request: %w", err)
	}
	defer resp.Body.Close()

	// Absorb any cookies from response
	s.absorbSetCookies(resp)

	log.Printf("[Session] %s: Init response status=%d, body=%d bytes", s.ID, resp.StatusCode, resp.ContentLength)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("gemini init returned status %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read init body: %w", err)
	}
	body := string(bodyBytes)

	log.Printf("[Session] %s: Init body size=%d bytes, has_SNlM0e=%v", s.ID, len(bodyBytes),
		strings.Contains(body, "SNlM0e"))

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

	log.Printf("[Session] %s: SNlM0e acquired, build=%s", s.ID, bl)
	return nil
}

// SelfHeal attempts to re-init the session when SNlM0e is stale or expired.
// Returns true if self-heal succeeded.
func (s *AccountSession) SelfHeal(ctx context.Context) bool {
	log.Printf("[Session] %s: attempting self-heal (re-fetching SNlM0e)", s.ID)
	err := s.Init(ctx)
	if err != nil {
		log.Printf("[Session] %s: self-heal failed: %v", s.ID, err)
		return false
	}
	log.Printf("[Session] %s: self-heal succeeded", s.ID)
	return true
}
