package service

import (
	"context"
	"fmt"
	"time"
)

const ReasoningAutoExcludeKey = "reasoning_auto_exclude"
const ReasoningLatestKey = "reasoning_latest"

func reasoningSnapshot(result *ScheduledTestResult) map[string]any {
	EvaluateReasoningProbe(result)
	return map[string]any{ReasoningLatestKey: map[string]any{
		"verdict": result.Quality.Verdict, "checked_at": result.FinishedAt.UTC().Format(time.RFC3339Nano),
		"model": result.ModelID, "result_id": result.ID,
	}}
}

// This policy never changes the administrator's schedulable flag or health status.
// Inconclusive/untested accounts stay eligible. A newer successful probe recovers automatically.
func (a *Account) IsReasoningExcluded() bool {
	enabled, _ := a.Extra[ReasoningAutoExcludeKey].(bool)
	latest, _ := a.Extra[ReasoningLatestKey].(map[string]any)
	return enabled && latest["verdict"] == "degraded"
}

func (s *ScheduledTestService) SetReasoningPolicy(ctx context.Context, accountID int64, enabled bool) error {
	if s.accountRepo == nil {
		return fmt.Errorf("account repository unavailable")
	}
	if s.db != nil {
		release, acquired, err := lockScheduledAccount(ctx, s.db, accountID)
		if err != nil {
			return err
		}
		if !acquired {
			return fmt.Errorf("account probe is running; retry after completion")
		}
		defer release()
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return err
	}
	if account.Platform != PlatformOpenAI {
		return fmt.Errorf("reasoning policy requires an OpenAI account")
	}
	// Take the newest result across all plans, not the first plan returned by SQL.
	items, err := s.ReasoningOverview(ctx)
	if err != nil {
		return err
	}
	var latest *ScheduledTestResult
	for _, item := range items {
		if item.AccountID == accountID && item.Result != nil && (latest == nil || item.Result.ID > latest.ID) {
			latest = item.Result
		}
	}
	updates := map[string]any{ReasoningAutoExcludeKey: enabled}
	if latest != nil {
		updates[ReasoningLatestKey] = reasoningSnapshot(latest)[ReasoningLatestKey]
	}
	return s.accountRepo.UpdateExtra(ctx, accountID, updates)
}
