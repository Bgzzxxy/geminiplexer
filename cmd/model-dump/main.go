package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"geminiplexer/internal/config"
	"geminiplexer/internal/gemini"
	"geminiplexer/internal/transport"
)

func main() {
	cfg, err := config.LoadConfig(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	acc := cfg.Accounts[0]
	s := gemini.NewAccountSession(acc)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.Init(ctx); err != nil {
		log.Fatalf("init: %v", err)
	}

	// raw fetch of otAQ7b to dump the full model registry
	token := s.GetToken()
	cookie := s.GetFullCookieHeader()
	authUser := s.AuthUser

	endpoint := "https://gemini.google.com/_/BardChatUi/data/batchexecute"
	if authUser != "" {
		endpoint = fmt.Sprintf("https://gemini.google.com/u/%s/_/BardChatUi/data/batchexecute", authUser)
	}
	q := url.Values{
		"rpcids":      {"otAQ7b"},
		"hl":          {"en"},
		"_reqid":      {strconv.Itoa(rand.Intn(90000) + 10000)},
		"rt":          {"c"},
		"source-path": {"/app"},
	}
	if authUser != "" {
		q.Set("source-path", "/u/"+authUser+"/app")
	}
	if s.BuildLabel != "" {
		q.Set("bl", s.BuildLabel)
	}
	if s.SessionID != "" {
		q.Set("f.sid", s.SessionID)
	}
	form := url.Values{"at": {token}, "f.req": {`[[["otAQ7b","[]",null,"generic"]]]`}}
	req, _ := http.NewRequestWithContext(ctx, "POST", endpoint+"?"+q.Encode(), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=utf-8")
	req.Header.Set("Origin", "https://gemini.google.com")
	req.Header.Set("Referer", "https://gemini.google.com/")
	transport.SetChromeHeaders(req)
	req.Header.Set("X-Same-Domain", "1")
	req.Header.Set("Cookie", cookie)
	if a := s.SAPISIDHash("https://gemini.google.com"); a != "" {
		req.Header.Set("Authorization", a)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	body := strings.TrimSpace(string(b))
	fmt.Println("status", resp.StatusCode, "len", len(body))
	if strings.HasPrefix(body, ")]}'") {
		body = strings.TrimSpace(strings.TrimPrefix(body, ")]}'"))
	}
	// skip length line
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || isDigit(line) {
			continue
		}
		var env []any
		if json.Unmarshal([]byte(line), &env) != nil {
			continue
		}
		for _, item := range env {
			ia, ok := item.([]any)
			if !ok || len(ia) < 3 {
				continue
			}
			name, _ := ia[1].(string)
			if name != "otAQ7b" {
				continue
			}
			data, _ := ia[2].(string)
			var p []any
			json.Unmarshal([]byte(data), &p)
			fmt.Printf("payload len=%d\n", len(p))
			if len(p) > 16 {
				fmt.Printf("tierFlags=%v\n", p[16])
			}
			if len(p) > 17 {
				fmt.Printf("capFlags=%v\n", p[17])
			}
			if len(p) > 24 {
				if b, err := json.Marshal(p[24]); err == nil {
					fmt.Printf("thinkingPolicy=%s\n", string(b))
				}
			}
			if len(p) > 15 {
				raw, ok := p[15].([]any)
				if ok {
					for i, mdAny := range raw {
						md, _ := mdAny.([]any)
						if len(md) == 0 {
							continue
						}
						b, _ := json.Marshal(md)
						fmt.Printf("  [%d] %s\n", i, string(b))
					}
				}
			}
		}
		return
	}
}

func g(a []any, i int) any {
	if i >= 0 && i < len(a) {
		return a[i]
	}
	return nil
}

func isDigit(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil
}

var _ = gemini.QuotaFreshness
