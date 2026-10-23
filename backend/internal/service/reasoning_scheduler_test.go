//go:build unit

package service

import (
	"context"
	"errors"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func reasoningTestAccount(id int64, verdict string) Account {
	return Account{ID: id, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true, Extra: map[string]any{ReasoningRoutingKey: "prefer", ReasoningLatestKey: map[string]any{"verdict": verdict, "checked_at": time.Now().Format(time.RFC3339Nano)}}}
}
func TestReasoningTierStalenessAndLastValid(t *testing.T) {
	a := reasoningTestAccount(1, "not_degraded")
	require.Equal(t, 0, a.ReasoningTier(time.Now()))
	a.Extra["reasoning_last_valid"] = a.Extra[ReasoningLatestKey]
	a.Extra[ReasoningLatestKey] = map[string]any{"verdict": "inconclusive"}
	require.Equal(t, 0, a.ReasoningTier(time.Now()))
	require.Equal(t, 1, a.ReasoningTier(time.Now().Add(49*time.Hour)))
	a.Extra[ReasoningRoutingKey] = "off"
	require.Equal(t, 0, a.ReasoningTier(time.Now()))
}
func TestReasoningTierSelection(t *testing.T) {
	for _, tc := range []struct {
		name         string
		busy, failed bool
		excluded     map[int64]struct{}
		want         int64
	}{
		{name: "healthy defeats lower sticky/priority", want: 1},
		{name: "busy healthy permits unknown", busy: true, want: 2},
		{name: "unavailable healthy permits unknown", failed: true, want: 2},
		{name: "retry skips failed accounts and degrades", excluded: map[int64]struct{}{1: {}, 2: {}}, want: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			accounts := []Account{reasoningTestAccount(3, "degraded"), reasoningTestAccount(2, "untested"), reasoningTestAccount(1, "not_degraded")}
			selectFn := func(_ context.Context, exclude map[int64]struct{}) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
				for i := range accounts {
					a := &accounts[i]
					if _, skip := exclude[a.ID]; skip {
						continue
					}
					if a.ID == 1 && tc.failed {
						return nil, OpenAIAccountScheduleDecision{}, ErrNoAvailableAccounts
					}
					if a.ID == 1 && tc.busy {
						return &AccountSelectionResult{Account: a, WaitPlan: &AccountWaitPlan{}}, OpenAIAccountScheduleDecision{}, nil
					}
					return &AccountSelectionResult{Account: a, Acquired: true}, OpenAIAccountScheduleDecision{}, nil
				}
				return nil, OpenAIAccountScheduleDecision{}, ErrNoAvailableAccounts
			}
			result, d, err := selectReasoningTiers(context.Background(), accounts, tc.excluded, 0, selectFn)
			require.NoError(t, err)
			require.Equal(t, tc.want, result.Account.ID)
			require.Equal(t, int(tc.want-1), d.ReasoningTier)
			if tc.want > 1 {
				require.NotEmpty(t, d.ReasoningFallbackReason)
			}
		})
	}
}
func TestReasoningTierWaitRecoveryCancellationAndHealth(t *testing.T) {
	a := reasoningTestAccount(1, "not_degraded")
	b := reasoningTestAccount(2, "degraded")
	calls := 0
	fn := func(_ context.Context, excluded map[int64]struct{}) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
		calls++
		require.NotContains(t, excluded, a.ID)
		if calls == 1 {
			return &AccountSelectionResult{Account: &a, WaitPlan: &AccountWaitPlan{}}, OpenAIAccountScheduleDecision{}, nil
		}
		return &AccountSelectionResult{Account: &a, Acquired: true}, OpenAIAccountScheduleDecision{}, nil
	}
	r, _, err := selectReasoningTiers(context.Background(), []Account{a, b}, nil, 300*time.Millisecond, fn)
	require.NoError(t, err)
	require.Equal(t, int64(1), r.Account.ID)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls = 0
	_, _, err = selectReasoningTiers(ctx, []Account{a, b}, nil, time.Second, fn)
	require.ErrorIs(t, err, context.Canceled)
	a.Schedulable = false
	b.Extra[ReasoningAutoExcludeKey] = true
	_, _, err = selectReasoningTiers(context.Background(), []Account{a, b}, nil, 0, fn)
	require.True(t, errors.Is(err, ErrNoAvailableAccounts))
}
