package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"geminiplexer/internal/config"
	"geminiplexer/internal/gemini"
)

func main() {
	cfg, err := config.LoadConfig(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	if len(cfg.Accounts) == 0 {
		log.Fatal("no accounts")
	}
	acc := cfg.Accounts[0]
	s := gemini.NewAccountSession(acc)
	s.SetConfigPath(os.Args[1])

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.Init(ctx); err != nil {
		log.Fatalf("init: %v", err)
	}

	buckets, err := s.CheckQuota(ctx)
	if err != nil {
		log.Fatalf("CheckQuota: %v", err)
	}
	for _, b := range buckets {
		fmt.Printf("%s: rem=%d total=%d unlimited=%v exhausted=%v reset=%s\n",
			b.Label, b.Remaining, b.Total, b.Unlimited, b.IsExhausted, b.ResetAt.Format(time.RFC3339))
	}
}