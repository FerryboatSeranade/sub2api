//go:build unit

package service

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestBillingCacheMergePendingAndExtraQuota(t *testing.T) {
	for _, tt := range []struct {
		name                        string
		usage, extra, used, pending float64
		wantErr                     bool
	}{
		{"regular pending exceeds", 4, 0, 0, 2, true},
		{"regular exact remaining", 4, 0, 0, 1, false},
		{"regular plus extra covers pending", 4, 10, 9, 2, false},
		{"extra covers pending", 5, 10, 8, 2, false},
		{"extra rejects pending overdraft", 5, 10, 9, 2, true},
		{"extra exhausted", 5, 10, 10, 0, true},
		{"extra remains usable", 5, 10, 9, 0, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc := &BillingCacheService{}
			key := &APIKey{ID: 1, RateLimit5h: 5, ExtraQuota: tt.extra}
			now := time.Now()
			err := svc.evaluateRateLimits(context.Background(), key, tt.usage, 0, 0, &now, &now, &now, tt.used, tt.pending)
			if tt.wantErr {
				require.ErrorIs(t, err, ErrAPIKeyRateLimit5hExceeded)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
