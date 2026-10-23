package service

import (
	"context"
	"errors"
	"math"
	"time"
)

// Explicit allowlist: never relay VM details, credentials, or management responses.
type VMQuotaWindow struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    string   `json:"resets_at,omitempty"`
}
type VMQuotaCredits struct {
	AvailableCount *int `json:"available_count"`
	Credits        []struct {
		ExpiresAt string `json:"expires_at"`
	} `json:"credits,omitempty"`
	FetchedAt string `json:"fetched_at,omitempty"`
}
type VMQuotaStatus struct {
	Source       string          `json:"source"`
	VMID         string          `json:"vm_id"`
	Plan         string          `json:"plan,omitempty"`
	FiveHour     VMQuotaWindow   `json:"five_hour"`
	SevenDay     VMQuotaWindow   `json:"seven_day"`
	ResetCredits *VMQuotaCredits `json:"reset_credits,omitempty"`
	SyncedAt     time.Time       `json:"synced_at"`
	RefreshedAt  *time.Time      `json:"refreshed_at,omitempty"`
	Error        string          `json:"error,omitempty"`
	Stale        bool            `json:"stale"`
	attemptedAt  time.Time
}
type vmQuotaItem struct {
	ID           string          `json:"id"`
	Platform     string          `json:"platform"`
	Plan         string          `json:"plan_type"`
	PlanFallback string          `json:"chatgpt_plan_type"`
	U5           *float64        `json:"utilization_5h"`
	U7           *float64        `json:"utilization_7d"`
	R5           string          `json:"reset_5h"`
	R7           string          `json:"reset_7d"`
	Credits      *VMQuotaCredits `json:"reset_credits"`
}

func vmQuotaPercent(v *float64) *float64 {
	if v == nil || math.IsNaN(*v) || math.IsInf(*v, 0) || *v < 0 {
		return nil
	}
	p := math.Min(*v*100, 100)
	return &p
}
func (v vmQuotaItem) status(now time.Time) VMQuotaStatus {
	plan := v.Plan
	if plan == "" {
		plan = v.PlanFallback
	}
	return VMQuotaStatus{Source: "vm2api", VMID: v.ID, Plan: plan,
		FiveHour: VMQuotaWindow{vmQuotaPercent(v.U5), v.R5}, SevenDay: VMQuotaWindow{vmQuotaPercent(v.U7), v.R7}, ResetCredits: v.Credits, SyncedAt: now}
}
func (s *VM2APIBridge) Quota(ctx context.Context, id int64, force bool) (VMQuotaStatus, error) {
	var vmID string
	// Resolve only the stored binding, never accept a VM identifier from the browser.
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(extra->>'vm2api_vm_id','') FROM accounts WHERE id=$1 AND deleted_at IS NULL AND platform='openai' AND type='apikey'`, id).Scan(&vmID)
	if err != nil || !bridgeSafeID.MatchString(vmID) {
		return VMQuotaStatus{}, errors.New("账号不存在或没有有效的 vm2api 容器绑定")
	}
	if !force {
		return s.cachedQuota(ctx, vmID), nil
	}
	// Concurrent clicks share one upstream query; failed requests are not retried automatically.
	ch := s.quotaRefresh.DoChan(vmID, func() (any, error) {
		s.quotaMu.Lock()
		previous, ok := s.quotaAttempts[vmID]
		s.quotaMu.Unlock()
		if ok && time.Since(previous.attemptedAt) < 10*time.Second {
			return previous, nil
		}
		work, cancel := context.WithTimeout(context.Background(), 55*time.Second)
		defer cancel()
		s.quotaOnce.Do(func() { s.quotaSlots = make(chan struct{}, 4) })
		select {
		case s.quotaSlots <- struct{}{}:
			defer func() { <-s.quotaSlots }()
		case <-work.Done():
			return nil, errors.New("额度刷新繁忙，请稍后再试")
		}
		status := s.cachedQuota(work, vmID)
		var result struct {
			Data struct {
				Plan  string `json:"plan_type"`
				Quota struct {
					U5 *float64 `json:"utilization_5h"`
					U7 *float64 `json:"utilization_7d"`
					R5 string   `json:"reset_5h"`
					R7 string   `json:"reset_7d"`
				} `json:"quota"`
				Credits *VMQuotaCredits `json:"reset_credits"`
				Warning string          `json:"warning"`
			} `json:"data"`
		}
		err := s.call(work, "POST", "/api/panel/vms/"+vmID+"/openai-quota/refresh", map[string]any{}, &result)
		now := time.Now()
		if err != nil {
			status.Error = err.Error()
			status.Stale = true
		} else {
			d := result.Data
			status = vmQuotaItem{ID: vmID, Plan: d.Plan, U5: d.Quota.U5, U7: d.Quota.U7, R5: d.Quota.R5, R7: d.Quota.R7, Credits: d.Credits}.status(now)
			status.RefreshedAt = &now
			if d.Warning != "" {
				status.Error = "额度已刷新，但重置券详情暂未更新"
			}
		}
		status.attemptedAt = now
		s.quotaMu.Lock()
		if s.quotaAttempts == nil {
			s.quotaAttempts = map[string]VMQuotaStatus{}
		}
		s.quotaAttempts[vmID] = status
		if s.quotaItems == nil {
			s.quotaItems = map[string]VMQuotaStatus{}
		}
		s.quotaItems[vmID] = status
		s.quotaMu.Unlock()
		return status, nil
	})
	select {
	case <-ctx.Done():
		return VMQuotaStatus{}, ctx.Err()
	case res := <-ch:
		if res.Err != nil {
			return VMQuotaStatus{}, res.Err
		}
		return res.Val.(VMQuotaStatus), nil
	}
}
func (s *VM2APIBridge) cachedQuota(ctx context.Context, vmID string) VMQuotaStatus {
	s.quotaMu.Lock()
	defer s.quotaMu.Unlock()
	now := time.Now()
	if now.After(s.quotaUntil) {
		work, cancel := context.WithTimeout(ctx, 12*time.Second)
		defer cancel()
		var list struct {
			Data struct {
				Items []vmQuotaItem `json:"items"`
			} `json:"data"`
		}
		err := s.call(work, "GET", "/api/panel/vms", nil, &list)
		s.quotaUntil = now.Add(30 * time.Second)
		if err != nil {
			s.quotaError = err.Error()
			s.quotaUntil = now.Add(10 * time.Second)
		} else {
			items := map[string]VMQuotaStatus{}
			for _, v := range list.Data.Items {
				if v.Platform == "openai" {
					st := v.status(now)
					st.RefreshedAt = s.quotaItems[v.ID].RefreshedAt
					items[v.ID] = st
				}
			}
			s.quotaItems = items
			s.quotaError = ""
		}
	}
	st, ok := s.quotaItems[vmID]
	if !ok {
		st = VMQuotaStatus{Source: "vm2api", VMID: vmID, Error: "vm2api 中未找到对应容器，请检查账号绑定", Stale: true}
	}
	if s.quotaError != "" {
		st.Error = s.quotaError
		st.Stale = true
	}
	return st
}
