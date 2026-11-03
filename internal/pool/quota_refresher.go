package pool

import (
	"context"
	"log"
	"time"
)

// StartQuotaRefresher periodically refreshes each account's quota buckets so the
// acquisition preflight can skip accounts whose requested model family is
// exhausted. Interval is shorter than QuotaFreshness so the cache stays warm.
func (p *AccountPool) StartQuotaRefresher(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	go func() {
		for {
			select {
			case <-time.After(interval):
				for _, s := range p.GetSessions() {
					if !s.IsHealthy {
						continue
					}
					buckets, err := s.CheckQuota(ctx)
					if err != nil {
						log.Printf("[Pool] quota refresh failed for %s: %v", s.ID, err)
						continue
					}
					if len(buckets) == 0 {
						log.Printf("[Pool] quota refresh for %s: no buckets returned", s.ID)
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}