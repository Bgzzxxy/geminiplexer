//go:build !windows

package main

import (
	"os"
	"os/signal"
	"syscall"

	"gemini-relay/internal/pool"
)

func setupReloadSignal(configPath string, accountPool *pool.AccountPool) {
	sighupChan := make(chan os.Signal, 1)
	signal.Notify(sighupChan, syscall.SIGHUP)
	go func() {
		for range sighupChan {
			reloadConfiguration(configPath, accountPool)
		}
	}()
}
