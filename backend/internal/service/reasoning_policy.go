package service

import (
	"context"
	"encoding/json"
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

const ReasoningPolicyKey = "reasoning_exclusion_policy"
const ReasoningGlobalKey = "reasoning_auto_exclude_global"

func reasoningPolicyEnabled(mode string, global bool) bool {
	return mode == "enabled" || (mode != "disabled" && global)
}

func (s *ScheduledTestService) GetReasoningGlobal(ctx context.Context) (bool, error) {
	var enabled bool
	err := s.db.QueryRowContext(ctx, "SELECT COALESCE((SELECT value = 'true' FROM settings WHERE key = $1), false)", ReasoningGlobalKey).Scan(&enabled)
	return enabled, err
}

// All policy changes and result snapshots share a transaction lock. Materializing the
// effective flag preserves scheduling compatibility during blue/green overlap.
func (s *ScheduledTestService) updateReasoningPolicy(ctx context.Context, accountID int64, mode string, global *bool, snapshot map[string]any) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(7318999999999999)"); err != nil {
		return err
	}
	if global != nil {
		_, err = tx.ExecContext(ctx, "INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, NOW()) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()", ReasoningGlobalKey, fmt.Sprint(*global))
		if err != nil {
			return err
		}
	}
	var enabled bool
	if err = tx.QueryRowContext(ctx, "SELECT COALESCE((SELECT value = 'true' FROM settings WHERE key = $1), false)", ReasoningGlobalKey).Scan(&enabled); err != nil {
		return err
	}
	if mode != "" {
		result, err := tx.ExecContext(ctx, "UPDATE accounts SET extra = COALESCE(extra, '{}'::jsonb) || jsonb_build_object('reasoning_exclusion_policy', $1::text) WHERE id = $2 AND platform = 'openai' AND deleted_at IS NULL", mode, accountID)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("OpenAI account not found")
		}
	}
	if snapshot != nil {
		raw, err := json.Marshal(snapshot)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE accounts SET extra = COALESCE(extra, '{}'::jsonb) || $1::jsonb WHERE id = $2 AND deleted_at IS NULL AND COALESCE((extra->'reasoning_latest'->>'result_id')::bigint, 0) <= COALESCE(($1::jsonb->'reasoning_latest'->>'result_id')::bigint, 0)", string(raw), accountID); err != nil {
			return err
		}
	}
	// Legacy true is retained as an explicit override; legacy false becomes inherit.
	_, err = tx.ExecContext(ctx, `UPDATE accounts SET extra = COALESCE(extra, '{}'::jsonb) || jsonb_build_object(
 'reasoning_exclusion_policy', COALESCE(extra->>'reasoning_exclusion_policy', CASE WHEN extra->>'reasoning_auto_exclude' = 'true' THEN 'enabled' ELSE 'inherit' END),
 'reasoning_auto_exclude', CASE COALESCE(extra->>'reasoning_exclusion_policy', CASE WHEN extra->>'reasoning_auto_exclude' = 'true' THEN 'enabled' ELSE 'inherit' END) WHEN 'enabled' THEN true WHEN 'disabled' THEN false ELSE $1 END), updated_at = NOW()
 WHERE platform = 'openai' AND deleted_at IS NULL AND ($2::bigint = 0 OR id = $2)`, enabled, accountID)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO scheduler_outbox (event_type, account_id, payload) SELECT 'account_changed', id, '{}'::jsonb FROM accounts WHERE platform = 'openai' AND deleted_at IS NULL AND ($1::bigint = 0 OR id = $1)", accountID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *ScheduledTestService) SetReasoningGlobal(ctx context.Context, enabled bool) error {
	// Populate accounts that have not been tested again since the status feature landed.
	items, err := s.ReasoningOverview(ctx)
	if err != nil {
		return err
	}
	latest := map[int64]*ScheduledTestResult{}
	for _, item := range items {
		if item.Result != nil && (latest[item.AccountID] == nil || item.Result.ID > latest[item.AccountID].ID) {
			latest[item.AccountID] = item.Result
		}
	}
	for id, result := range latest {
		if err := s.saveReasoningSnapshot(ctx, id, result); err != nil {
			return err
		}
	}
	return s.updateReasoningPolicy(ctx, 0, "", &enabled, nil)
}

func (s *ScheduledTestService) saveReasoningSnapshot(ctx context.Context, id int64, result *ScheduledTestResult) error {
	// Snapshot writes are serialized with switches. The latest database result is
	// selected by the caller; an older concurrent result must not replace a newer one.
	if s.db == nil {
		return s.accountRepo.UpdateExtra(ctx, id, reasoningSnapshot(result))
	}
	return s.updateReasoningPolicy(ctx, id, "", nil, reasoningSnapshot(result))
}

func (s *ScheduledTestService) SetReasoningPolicy(ctx context.Context, accountID int64, mode string) error {
	if mode != "inherit" && mode != "enabled" && mode != "disabled" {
		return fmt.Errorf("policy must be inherit, enabled or disabled")
	}
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
	var snapshot map[string]any
	if latest != nil {
		snapshot = reasoningSnapshot(latest)
	}
	return s.updateReasoningPolicy(ctx, accountID, mode, nil, snapshot)
}
