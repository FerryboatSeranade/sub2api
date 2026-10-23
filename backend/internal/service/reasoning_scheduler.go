package service

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

type reasoningSelectFunc func(context.Context, map[int64]struct{}) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error)

// Each tier runs the original scheduler with a disjoint candidate set. Load,
// model, quota, transport, health, permissions and acquisition are not bypassed.
func selectReasoningTiers(ctx context.Context, accounts []Account, excluded map[int64]struct{}, wait time.Duration, selectFn reasoningSelectFunc) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
	if len(accounts) == 0 {
		return selectFn(ctx, excluded)
	}
	now := time.Now()
	tiers := map[int64]int{}
	present := [3]bool{}
	for i := range accounts {
		a := &accounts[i]
		if _, skip := excluded[a.ID]; skip || !a.IsSchedulable() {
			continue
		}
		tier := a.ReasoningTier(now)
		tiers[a.ID] = tier
		present[tier] = true
	}
	var pending *AccountSelectionResult
	var pendingDecision OpenAIAccountScheduleDecision
	var lastErr error = ErrNoAvailableAccounts
	var lastDecision OpenAIAccountScheduleDecision
	deadline := time.Now().Add(wait)
	reason := "higher_tiers_unavailable"
	for tier := 0; tier < 3; tier++ {
		if !present[tier] {
			continue
		}
		blocked := make(map[int64]struct{}, len(accounts)+len(excluded))
		for id := range excluded {
			blocked[id] = struct{}{}
		}
		for _, a := range accounts {
			t, ok := tiers[a.ID]
			if !ok || t != tier {
				blocked[a.ID] = struct{}{}
			}
		}
		for {
			selected, decision, err := selectFn(ctx, blocked)
			decision.ReasoningTier = tier
			if tier > 0 {
				decision.ReasoningFallbackReason = reason
			}
			lastDecision = decision
			if err != nil {
				if !errors.Is(err, ErrNoAvailableAccounts) && !errors.Is(err, ErrNoAvailableCompactAccounts) {
					return selected, decision, err
				}
				lastErr = err
				break
			}
			if selected != nil && selected.Acquired {
				return selected, decision, nil
			}
			if selected != nil && selected.WaitPlan != nil {
				if pending == nil {
					pending = selected
					pendingDecision = decision
				}
				if tier < 2 && time.Now().Before(deadline) {
					timer := time.NewTimer(min(250*time.Millisecond, time.Until(deadline)))
					select {
					case <-ctx.Done():
						timer.Stop()
						return nil, decision, ctx.Err()
					case <-timer.C:
					}
					continue
				}
				reason = "higher_tiers_busy_after_wait"
			}
			break
		}
	}
	if pending != nil {
		return pending, pendingDecision, nil
	}
	return nil, lastDecision, lastErr
}

func (s *OpenAIGatewayService) selectAccountWithScheduler(
	ctx context.Context, groupID *int64, previousResponseID, sessionHash, requestedModel string,
	excludedIDs map[int64]struct{}, requiredTransport OpenAIUpstreamTransport,
	requiredCapability OpenAIEndpointCapability, requiredImageCapability OpenAIImagesCapability,
	requireCompact bool, platform string, previousResponseCanMove, useUpstreamTokenCost bool,
) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
	selectFn := func(ctx context.Context, excluded map[int64]struct{}) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
		return s.selectAccountWithoutReasoningTiers(ctx, groupID, previousResponseID, sessionHash, requestedModel, excluded, requiredTransport, requiredCapability, requiredImageCapability, requireCompact, platform, previousResponseCanMove, useUpstreamTokenCost)
	}
	// Account-owned continuations and diagnostics must keep their required owner.
	target, _ := ctx.Value(codexTicketDiagnosticTargetKey{}).(int64)
	if NormalizeOpenAICompatiblePlatform(platform) != PlatformOpenAI || (previousResponseID != "" && !previousResponseCanMove) || target > 0 || preserveOpenAIGuardianParentBinding(ctx, sessionHash) {
		return selectFn(ctx, excludedIDs)
	}
	accounts, err := s.listSchedulableAccounts(ctx, groupID, platform)
	if err != nil {
		return nil, OpenAIAccountScheduleDecision{}, err
	}
	selected, decision, err := selectReasoningTiers(ctx, accounts, excludedIDs, 2*time.Second, selectFn)
	if err == nil && selected != nil && selected.Account != nil {
		// Copy before adding request-local annotations: cached accounts are shared.
		account := *selected.Account
		account.ReasoningRoutingTier = decision.ReasoningTier
		account.ReasoningFallbackReason = decision.ReasoningFallbackReason
		selected.Account = &account
		if decision.ReasoningTier > 0 {
			slog.InfoContext(ctx, "reasoning routing fallback", "account_id", account.ID, "tier", decision.ReasoningTier, "reason", decision.ReasoningFallbackReason)
		}
	}
	return selected, decision, err
}

func reasoningRouteSnapshot(account *Account) *string {
	if account == nil || account.Platform != PlatformOpenAI {
		return nil
	}
	value := "质量优先 / 原有调度"
	if account.ReasoningFallbackReason != "" {
		value = "未知质量候补"
		if account.ReasoningRoutingTier == 2 {
			value = "降智兜底"
		}
		if account.ReasoningFallbackReason == "higher_tiers_busy_after_wait" {
			value += "：优先层并发等待超时"
		} else {
			value += "：优先层无可用账号（额度、限流、故障或权限限制）"
		}
	}
	return &value
}
