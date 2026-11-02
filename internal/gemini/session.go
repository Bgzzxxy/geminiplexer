package gemini

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
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
	Secure1PSIDCC          string
	SID                    string
	HSID                   string
	SSID                   string
	APISID                 string
	SAPISID                string
	NID                    string
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
	Group                  string
	// Concurrency control: limits parallel requests per account
	concSem                chan struct{}
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

// applyRawCookies parses the full Google auth cookie set out of a raw cookie
// string and stores it on the session. Callers must hold s.mu (write lock).
func (s *AccountSession) applyRawCookies(rawCookies string) {
	s.RawCookies = rawCookies
	s.Secure1PSID = parseCookieField(rawCookies, "__Secure-1PSID")
	s.Secure1PSIDTS = parseCookieField(rawCookies, "__Secure-1PSIDTS")
	s.SIDCC = parseCookieField(rawCookies, "SIDCC")
	s.Secure1PSIDCC = parseCookieField(rawCookies, "__Secure-1PSIDCC")
	s.SID = parseCookieField(rawCookies, "SID")
	s.HSID = parseCookieField(rawCookies, "HSID")
	s.SSID = parseCookieField(rawCookies, "SSID")
	s.APISID = parseCookieField(rawCookies, "APISID")
	s.SAPISID = parseCookieField(rawCookies, "SAPISID")
	s.NID = parseCookieField(rawCookies, "NID")
}

func NewAccountSession(cfg config.AccountConfig) *AccountSession {
	// Create stealth client with Chrome uTLS fingerprint
	client, err := transport.NewStealthClient(cfg.Proxy, 5*time.Minute)
	if err != nil {
		log.Printf("[Session] Warning: failed to create stealth client for %s, falling back to standard: %v", cfg.ID, err)
		client = &http.Client{Timeout: 5 * time.Minute}
	}

	sess := &AccountSession{
		ID:        cfg.ID,
		AuthUser:  cfg.AuthUser,
		Client:    client,
		IsHealthy: true,
		ProxyURL:  cfg.Proxy,
		Group:     cfg.Group,
		concSem:   make(chan struct{}, 1), // 1 concurrent request per account
	}
	sess.applyRawCookies(cfg.Cookies)
	// Pre-fill the semaphore (available slot)
	sess.concSem <- struct{}{}
	return sess
}

func (s *AccountSession) SetEndpointOverride(u string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.EndpointOverride = u
}

func (s *AccountSession) UpdateCookies(rawCookies string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.applyRawCookies(rawCookies)
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

// SAPISIDHash computes the Authorization: SAPISIDHASH value required by Gemini
// Web's batchexecute endpoint (model discovery + quota RPCs). Formula matches
// gemini-stealth-bridge: sha1("<unix_ts> <SAPISID> <origin>").
func (s *AccountSession) SAPISIDHash(origin string) string {
	s.mu.RLock()
	sapisid := s.SAPISID
	s.mu.RUnlock()
	if sapisid == "" {
		return ""
	}
	if origin == "" {
		origin = "https://gemini.google.com"
	}
	ts := time.Now().Unix()
	sum := sha1.Sum([]byte(fmt.Sprintf("%d %s %s", ts, sapisid, origin)))
	return fmt.Sprintf("SAPISIDHASH %d_%s", ts, hex.EncodeToString(sum[:]))
}

func (s *AccountSession) getFullCookieHeaderLocked() string {
	var parts []string
	// Emit the full auth cookie set. Google's Gemini web app requires more than
	// just __Secure-1PSID/1PSIDTS/SIDCC to serve an authenticated /app page.
	pairs := []struct {
		name  string
		value string
	}{
		{"__Secure-1PSID", s.Secure1PSID},
		{"__Secure-1PSIDTS", s.Secure1PSIDTS},
		{"SIDCC", s.SIDCC},
		{"__Secure-1PSIDCC", s.Secure1PSIDCC},
		{"SID", s.SID},
		{"HSID", s.HSID},
		{"SSID", s.SSID},
		{"APISID", s.APISID},
		{"SAPISID", s.SAPISID},
		{"NID", s.NID},
	}
	for _, p := range pairs {
		if p.value != "" {
			parts = append(parts, p.name+"="+p.value)
		}
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
		case "__Secure-1PSIDCC":
			s.mu.Lock()
			s.Secure1PSIDCC = c.Value
			s.mu.Unlock()
		case "SID":
			s.mu.Lock()
			s.SID = c.Value
			s.mu.Unlock()
		case "HSID":
			s.mu.Lock()
			s.HSID = c.Value
			s.mu.Unlock()
		case "SSID":
			s.mu.Lock()
			s.SSID = c.Value
			s.mu.Unlock()
		case "APISID":
			s.mu.Lock()
			s.APISID = c.Value
			s.mu.Unlock()
		case "SAPISID":
			s.mu.Lock()
			s.SAPISID = c.Value
			s.mu.Unlock()
		case "NID":
			s.mu.Lock()
			s.NID = c.Value
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

// TryAcquire attempts to acquire the concurrency semaphore without blocking.
// Returns true if acquired, false if the account is busy.
func (s *AccountSession) TryAcquire() bool {
	select {
	case <-s.concSem:
		return true
	default:
		return false
	}
}

// AcquireWithContext blocks until the semaphore is available or context is cancelled.
func (s *AccountSession) AcquireWithContext(ctx context.Context) error {
	select {
	case <-s.concSem:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Release returns the concurrency semaphore slot.
func (s *AccountSession) Release() {
	select {
	case s.concSem <- struct{}{}:
	default:
		// Already released, shouldn't happen
	}
}

// IsBusy returns true if the account is currently handling a request.
func (s *AccountSession) IsBusy() bool {
	return len(s.concSem) == 0
}
