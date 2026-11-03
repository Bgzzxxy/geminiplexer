package gemini

import (
	"testing"
	"time"
)

// TestExtractQuotaBuckets verifies the precise qpEbW structure:
// payload=[items], item=[idList, ?, usagePct, resetArr, total, remaining],
// idList[1]=action id (4=Pro, 11=Flash).
func TestExtractQuotaBuckets(t *testing.T) {
	payload := []any{
		// payload[0] = items array (matching gsb: pArr[0] is the items slice)
		[]any{
			// item: [idList, ?, usagePct, resetArr, total, remaining]
			// idList: [something, 11] -> Flash
			[]any{
				[]any{float64(1), float64(11)},
				nil,
				float64(0.53),
				[]any{float64(1793678380)},
				float64(100),
				float64(40),
			},
			[]any{
				[]any{float64(1), float64(4)},
				nil,
				float64(0.80),
				[]any{float64(1793678380)},
				float64(50),
				float64(0),
			},
			// Action id not in map -> skipped.
			[]any{
				[]any{float64(2), float64(99)},
				nil,
				float64(0.1),
				[]any{float64(999)},
				float64(5),
				float64(5),
			},
		},
	}

	buckets := extractQuotaBuckets(payload)
	if len(buckets) != 2 {
		t.Fatalf("expected 2 buckets, got %d", len(buckets))
	}

	flash := buckets[0]
	if flash.Kind != "flash" || flash.Label != "Gemini Flash" {
		t.Fatalf("bucket 0 wrong: %+v", flash)
	}
	if flash.Remaining != 40 || flash.Total != 100 {
		t.Fatalf("flash numbers wrong: %+v", flash)
	}
	if flash.IsExhausted {
		t.Fatalf("flash (40/100) should not be exhausted")
	}
	if !flash.ResetAt.Equal(time.Unix(1793678380, 0)) {
		t.Fatalf("flash resetAt wrong: %v", flash.ResetAt)
	}

	pro := buckets[1]
	if pro.Kind != "pro" || !pro.IsExhausted {
		t.Fatalf("pro bucket wrong (should be exhausted): %+v", pro)
	}
}

func TestQuotaExhaustedFor(t *testing.T) {
	s := &AccountSession{}
	now := time.Now()

	// No data yet -> not known, must NOT skip.
	exh, _, known := s.QuotaExhaustedFor("gemini-4-argon", now, QuotaFreshness)
	if known || exh {
		t.Fatalf("no quota data: known=%v exh=%v, want known=false", known, exh)
	}

	// Fresh exhausted pro bucket.
	proExc := QuotaBucket{Kind: "pro", IsExhausted: true, ResetAt: now.Add(time.Hour)}
	s.SetCachedQuota([]QuotaBucket{
		{Kind: "flash", IsExhausted: false},
		proExc,
	}, now)

	exh, reset, known := s.QuotaExhaustedFor("gemini-4-argon", now, QuotaFreshness)
	if !known || !exh {
		t.Fatalf("pro exhausted: known=%v exh=%v, want true", known, exh)
	}
	if !reset.Equal(proExc.ResetAt) {
		t.Fatalf("reset mismatch: %v", reset)
	}

	// Flash request not exhausted.
	exh, _, _ = s.QuotaExhaustedFor("gemini-3.8-flash", now, QuotaFreshness)
	if exh {
		t.Fatal("flash should not be exhausted")
	}

	// Stale cache -> unknown again, must not skip.
	stale := now.Add(QuotaFreshness + time.Minute)
	exh, _, known = s.QuotaExhaustedFor("gemini-4-argon", stale, QuotaFreshness)
	if known || exh {
		t.Fatalf("stale cache: known=%v exh=%v, want known=false", known, exh)
	}
}

func TestQuotaKindForModel(t *testing.T) {
	if quotaKindForModel("gemini-4-argon") != "pro" {
		t.Fatal("argon should be pro family")
	}
	if quotaKindForModel("gemini-3.1-pro") != "pro" {
		t.Fatal("pro should be pro family")
	}
	if quotaKindForModel("gemini-3.8-flash") != "flash" {
		t.Fatal("flash should be flash family")
	}
	if quotaKindForModel("gemini-flash-lite") != "flash" {
		t.Fatal("flash-lite should be flash family")
	}
}
