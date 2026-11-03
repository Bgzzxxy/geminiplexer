package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"geminiplexer/internal/config"
	"geminiplexer/internal/gemini"
	"geminiplexer/internal/pool"
	"geminiplexer/internal/server"
)

func main() {
	configPath := flag.String("config", "accounts.yaml", "Path to YAML configuration file")
	flag.Parse()

	log.Printf("[Main] Loading configuration from %s", *configPath)
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("[Main] Failed to load config: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var sessions []*gemini.AccountSession
	var wg sync.WaitGroup

	log.Printf("[Main] Initializing %d accounts...", len(cfg.Accounts))
	for _, accCfg := range cfg.Accounts {
		sess := gemini.NewAccountSession(accCfg)
		sess.SetConfigPath(*configPath)
		sessions = append(sessions, sess)

		wg.Add(1)
		go func(s *gemini.AccountSession) {
			defer wg.Done()
			initCtx, initCancel := context.WithTimeout(ctx, 30*time.Second)
			defer initCancel()

			if err := s.Init(initCtx); err != nil {
				log.Printf("[Main] Account %s initial handshake failed: %v", s.ID, err)
			} else {
				log.Printf("[Main] Account %s handshake success (Token acquired)", s.ID)
				if _, err := s.FetchModels(initCtx); err != nil {
					log.Printf("[Main] Account %s model fetch warning: %v", s.ID, err)
				}
			}
		}(sess)
	}
	wg.Wait()

	accountPool := pool.NewAccountPool(sessions)
	accountPool.StartBackgroundRotator(ctx, 1440)

	srv := server.NewServer(cfg, accountPool, func() error {
		reloadConfiguration(*configPath, accountPool)
		return nil
	})

	setupReloadSignal(*configPath, accountPool)

	// Graceful shutdown listener
	shutdownChan := make(chan os.Signal, 1)
	signal.Notify(shutdownChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("[Main] Gemini-to-API Gateway listening on http://%s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[Main] Server error: %v", err)
		}
	}()

	<-shutdownChan
	log.Printf("[Main] Shutting down gracefully...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = srv.Shutdown(shutdownCtx)
	log.Printf("[Main] Server exited cleanly")
}

func reloadConfiguration(configPath string, accountPool *pool.AccountPool) {
	log.Printf("[Main] Reloading configuration from %s", configPath)
	newCfg, err := config.LoadConfig(configPath)
	if err != nil {
		log.Printf("[Main] Failed to reload config: %v", err)
		return
	}
	for _, acc := range newCfg.Accounts {
		found := false
		for _, s := range accountPool.GetSessions() {
			if s.ID == acc.ID {
				s.UpdateCookies(acc.Cookies)
				go s.Init(context.Background())
				found = true
				break
			}
		}
		if !found {
			newS := gemini.NewAccountSession(acc)
			go newS.Init(context.Background())
			accountPool.AddSession(newS)
		}
	}
	log.Printf("[Main] Configuration reloaded successfully")
}
