package pool

import (
	"context"
	"log"
	"math/rand"
	"time"
)

func (p *AccountPool) StartBackgroundRotator(ctx context.Context, baseIntervalMinutes int) {
	if baseIntervalMinutes <= 0 {
		baseIntervalMinutes = 1440 // default 24h
	}

	go func() {
		for {
			// Jitter: +/- 10%
			jitterSeconds := (rand.Float64()*0.2 - 0.1) * float64(baseIntervalMinutes*60)
			sleepDuration := time.Duration(float64(baseIntervalMinutes*60)+jitterSeconds) * time.Second

			select {
			case <-time.After(sleepDuration):
				sessions := p.GetSessions()
				for _, s := range sessions {
					if err := s.RotateCookies(ctx); err != nil {
						log.Printf("[Pool] Warning: cookie rotation failed for account %s: %v", s.ID, err)
					} else {
						log.Printf("[Pool] Cookie rotated successfully for account %s", s.ID)
					}
					// Jitter pause between accounts
					time.Sleep(time.Duration(5+rand.Intn(10)) * time.Second)
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}
