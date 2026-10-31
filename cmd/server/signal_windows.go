//go:build windows

package main

import (
	"gemini-relay/internal/pool"
)

func setupReloadSignal(configPath string, accountPool *pool.AccountPool) {
	// Windows does not support SIGHUP; configuration reloads are handled via POST /admin/reload
}
