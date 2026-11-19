package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"
)

const sourceTierKey = "source_tier_routing_v1"

type SourceTier struct {
	Source  string `json:"source"`
	Quality string `json:"quality"`
	Enabled bool   `json:"enabled"`
}
type SourceTierConfig struct {
	EntryScope          string       `json:"entry_scope"`
	ExcludedEntryGroups []int64      `json:"excluded_entry_groups"`
	Enabled             bool         `json:"enabled"`
	EntryGroups         []int64      `json:"entry_groups"`
	SelfGroups          []int64      `json:"self_groups"`
	ExternalGroups      []int64      `json:"external_groups"`
	Models              []string     `json:"models"`
	MaxAgeHours         int          `json:"max_age_hours"`
	Layers              []SourceTier `json:"layers"`
}
type sourceTierStore struct {
	db        *sql.DB
	mu        sync.Mutex
	config    SourceTierConfig
	until     time.Time
	qualities map[string]sourceTierQualityCache
}
type sourceTierQualityCache struct {
	values map[int64]sourceTierQuality
	until  time.Time
}

func defaultSourceTierConfig() SourceTierConfig {
	return SourceTierConfig{EntryScope: "all", ExcludedEntryGroups: []int64{}, EntryGroups: []int64{}, SelfGroups: []int64{}, ExternalGroups: []int64{}, Models: []string{"gpt-6.1-sol"}, MaxAgeHours: 48, Layers: []SourceTier{{"self", "not_degraded", true}, {"external", "not_degraded", true}, {"self", "degraded", true}}}
}
func (s *OpenAIGatewayService) SourceTierConfig(ctx context.Context) (SourceTierConfig, error) {
	if s.sourceTiers == nil {
		return defaultSourceTierConfig(), nil
	}
	st := s.sourceTiers
	st.mu.Lock()
	defer st.mu.Unlock()
	if time.Now().Before(st.until) {
		return st.config, nil
	}
	v := defaultSourceTierConfig()
	var raw string
	if err := st.db.QueryRowContext(ctx, "SELECT COALESCE((SELECT value FROM settings WHERE key=$1),'{}')", sourceTierKey).Scan(&raw); err != nil {
		return v, err
	}
	var err error
	v, err = decodeSourceTierConfig(raw)
	if err != nil {
		return v, err
	}
	st.config = v
	st.until = time.Now().Add(3 * time.Second)
	return v, nil
}

// Legacy explicitly selected or enabled configurations never silently expand.
func (v SourceTierConfig) entryScope() string {
	if v.EntryScope != "" {
		return v.EntryScope
	}
	if v.Enabled || len(v.EntryGroups) > 0 {
		return "selected"
	}
	return "all"
}
func decodeSourceTierConfig(raw string) (SourceTierConfig, error) {
	v := defaultSourceTierConfig()
	v.EntryScope = ""
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return v, err
	}
	v.EntryScope = v.entryScope()
	return v, nil
}
func (v SourceTierConfig) matchesEntryGroup(id int64) bool {
	if id <= 0 {
		return false
	}
	switch v.entryScope() {
	case "all":
		return true
	case "selected":
		return slices.Contains(v.EntryGroups, id)
	case "exclude":
		return !slices.Contains(v.ExcludedEntryGroups, id)
	default:
		return false
	}
}
func (s *OpenAIGatewayService) sourceTierEntryIsOpenAI(ctx context.Context, id int64) (bool, error) {
	var ok bool
	err := s.sourceTiers.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM groups WHERE id=$1 AND deleted_at IS NULL AND status='active' AND platform='openai')", id).Scan(&ok)
	return ok, err
}
func validateSourceTierConfig(v SourceTierConfig) error {
	if scope := v.entryScope(); scope != "all" && scope != "selected" && scope != "exclude" {
		return fmt.Errorf("无效生效范围")
	}
	if v.MaxAgeHours < 1 || v.MaxAgeHours > 720 {
		return fmt.Errorf("检测有效期须为 1–720 小时")
	}
	if len(v.Layers) == 0 || len(v.Layers) > 6 {
		return fmt.Errorf("层级数量须为 1–6")
	}
	seen := map[SourceTier]bool{}
	enabled := 0
	for _, l := range v.Layers {
		if (l.Source != "self" && l.Source != "external") || (l.Quality != "not_degraded" && l.Quality != "degraded" && l.Quality != "unknown") {
			return fmt.Errorf("无效来源或质量")
		}
		k := l
		k.Enabled = false
		if seen[k] {
			return fmt.Errorf("来源与质量组合不能重复")
		}
		seen[k] = true
		if l.Enabled {
			enabled++
		}
	}
	for _, id := range v.SelfGroups {
		if slices.Contains(v.ExternalGroups, id) {
			return fmt.Errorf("自建和外接分组不能重叠")
		}
	}
	for _, ids := range [][]int64{v.EntryGroups, v.ExcludedEntryGroups, v.SelfGroups, v.ExternalGroups} {
		if len(ids) > 100 {
			return fmt.Errorf("每类最多选择 100 个分组")
		}
		m := map[int64]bool{}
		for _, id := range ids {
			if id <= 0 || m[id] {
				return fmt.Errorf("分组 ID 无效或重复")
			}
			m[id] = true
		}
	}
	if len(v.Models) > 100 {
		return fmt.Errorf("最多配置 100 个模型")
	}
	models := map[string]bool{}
	for _, m := range v.Models {
		if strings.TrimSpace(m) != m || m == "" || strings.Contains(m, "*") || models[m] {
			return fmt.Errorf("模型须为不重复的精确名称")
		}
		models[m] = true
	}
	if v.Enabled && ((v.entryScope() == "selected" && len(v.EntryGroups) == 0) || len(v.Models) == 0 || enabled == 0) {
		return fmt.Errorf("启用前请选择生效分组、模型及层级")
	}
	if v.Enabled {
		for _, l := range v.Layers {
			if l.Enabled && len(v.groups(l.Source)) == 0 {
				return fmt.Errorf("启用层的来源分组不能为空")
			}
		}
	}
	return nil
}
func (v SourceTierConfig) groups(source string) []int64 {
	if source == "self" {
		return v.SelfGroups
	}
	return v.ExternalGroups
}
func (s *OpenAIGatewayService) SaveSourceTierConfig(ctx context.Context, v SourceTierConfig) error {
	v.EntryScope = v.entryScope()
	if err := validateSourceTierConfig(v); err != nil {
		return err
	}
	if s.sourceTiers == nil {
		return fmt.Errorf("调度配置存储未初始化")
	}
	if v.Enabled {
		groupsToValidate := [][]int64{v.SelfGroups, v.ExternalGroups}
		if v.EntryScope == "selected" {
			groupsToValidate = append(groupsToValidate, v.EntryGroups)
		}
		for _, ids := range groupsToValidate {
			for _, id := range ids {
				var ok bool
				if err := s.sourceTiers.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM groups WHERE id=$1 AND deleted_at IS NULL AND status='active' AND platform='openai')", id).Scan(&ok); err != nil {
					return err
				}
				if !ok {
					return fmt.Errorf("分组 #%d 不存在、未启用或不是 OpenAI 分组", id)
				}
			}
		}
	}

	raw, _ := json.Marshal(v)
	_, err := s.sourceTiers.db.ExecContext(ctx, "INSERT INTO settings(key,value,updated_at) VALUES($1,$2,NOW()) ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value,updated_at=NOW()", sourceTierKey, string(raw))
	if err == nil {
		s.sourceTiers.mu.Lock()
		s.sourceTiers.until = time.Time{}
		s.sourceTiers.mu.Unlock()
	}
	return err
}

type sourceTierQuality struct {
	Verdict string
	At      time.Time
}

func (s *OpenAIGatewayService) sourceTierQualities(ctx context.Context, model string) (map[int64]sourceTierQuality, error) {
	st := s.sourceTiers
	st.mu.Lock()
	defer st.mu.Unlock()
	if c, ok := st.qualities[model]; ok && time.Now().Before(c.until) {
		return c.values, nil
	}
	out := map[int64]sourceTierQuality{}
	rows, err := s.sourceTiers.db.QueryContext(ctx, `SELECT DISTINCT ON(p.account_id) p.account_id,row_to_json(r)
 FROM scheduled_test_results r JOIN scheduled_test_plans p ON p.id=r.plan_id
 WHERE r.test_kind='candy_v1' AND r.model_id=$1 AND r.status='success'
 ORDER BY p.account_id,r.finished_at DESC,r.id DESC`, model)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var raw []byte
		if err = rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		var r ScheduledTestResult
		if err = json.Unmarshal(raw, &r); err != nil {
			return nil, err
		}
		EvaluateReasoningProbe(&r)
		if r.Quality != nil {
			out[id] = sourceTierQuality{r.Quality.Verdict, r.FinishedAt}
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if st.qualities == nil {
		st.qualities = map[string]sourceTierQualityCache{}
	}
	if len(st.qualities) > 100 {
		st.qualities = map[string]sourceTierQualityCache{}
	}
	st.qualities[model] = sourceTierQualityCache{out, time.Now().Add(5 * time.Second)}
	return out, nil
}
func sourceTierVerdict(q sourceTierQuality, maxAge int, now time.Time) string {
	if q.At.IsZero() || q.At.After(now.Add(time.Minute)) || now.Sub(q.At) > time.Duration(maxAge)*time.Hour {
		return "unknown"
	}
	if q.Verdict == "not_degraded" || q.Verdict == "degraded" {
		return q.Verdict
	}
	return "unknown"
}

// Source groups grant access only through an explicit entry-group policy.
// Calls retain the original request's billing/authentication group; only account selection uses a source group.
type sourceTierSelectFunc func(context.Context, *int64, string, map[int64]struct{}) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error)

func (s *OpenAIGatewayService) selectSourceTiers(ctx context.Context, v SourceTierConfig, entry int64, model, session string, excluded map[int64]struct{}, owned bool, selectFn sourceTierSelectFunc) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
	quality, err := s.sourceTierQualities(ctx, model)
	if err != nil {
		return nil, OpenAIAccountScheduleDecision{}, err
	}
	var pending *AccountSelectionResult
	var pd OpenAIAccountScheduleDecision
	for n, layer := range v.Layers {
		if !layer.Enabled {
			continue
		}
		for _, id := range v.groups(layer.Source) {
			// Recheck group state each selection: deleted/disabled groups cannot keep granting access.
			var active bool
			if err = s.sourceTiers.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM groups WHERE id=$1 AND deleted_at IS NULL AND status='active' AND platform='openai')", id).Scan(&active); err != nil {
				return nil, pd, err
			}
			if !active {
				continue
			}
			accounts, e := s.listSchedulableAccounts(ctx, &id, PlatformOpenAI)
			if e != nil {
				return nil, pd, e
			}
			blocked := map[int64]struct{}{}
			for k := range excluded {
				blocked[k] = struct{}{}
			}
			eligible := 0
			allowed := map[int64]bool{}
			for _, a := range accounts {
				conflict := false
				for _, g := range a.GroupIDs {
					if slices.Contains(v.SelfGroups, g) && layer.Source != "self" || slices.Contains(v.ExternalGroups, g) && layer.Source != "external" {
						conflict = true
					}
				}
				if conflict || (!owned && sourceTierVerdict(quality[a.ID], v.MaxAgeHours, time.Now()) != layer.Quality) || (s.openAIGroupRequiresPrivacySet(ctx, &entry) && !a.IsPrivacySet()) {
					blocked[a.ID] = struct{}{}
				}
				if _, no := blocked[a.ID]; !no {
					eligible++
					allowed[a.ID] = true
				}
			}
			if eligible == 0 {
				continue
			}
			// Entry-scoped session namespace prevents unrelated entry groups sharing sticky bindings.
			scoped := session
			if scoped != "" {
				scoped = fmt.Sprintf("tier:%d:%s", entry, session)
			}
			r, d, e := selectFn(ctx, &id, scoped, blocked)
			if e != nil {
				if errors.Is(e, ErrNoAvailableAccounts) || errors.Is(e, ErrNoAvailableCompactAccounts) {
					continue
				}
				return nil, d, e
			}
			if r != nil && r.Account != nil && (!allowed[r.Account.ID] || (s.openAIGroupRequiresPrivacySet(ctx, &entry) && !r.Account.IsPrivacySet()) || (owned && !d.StickyPreviousHit && d.Layer != openAIAccountScheduleLayerGuardianParent)) {
				if r.ReleaseFunc != nil {
					r.ReleaseFunc()
				}
				continue
			}
			d.ReasoningTier = n
			if n > 0 {
				d.ReasoningFallbackReason = "source_tiers_unavailable"
			}
			if r != nil && r.Account != nil {
				copy := *r.Account
				copy.ReasoningRoutingTier = n
				copy.ReasoningFallbackReason = d.ReasoningFallbackReason
				sourceLabel := map[string]string{"self": "自建", "external": "外接"}[layer.Source]
				qualityLabel := map[string]string{"not_degraded": "不降智", "degraded": "降智", "unknown": "质量未知"}[layer.Quality]
				copy.SourceRoutingLabel = fmt.Sprintf("第 %d 层 / %s / %s", n+1, sourceLabel, qualityLabel)
				r.Account = &copy
			}
			if r != nil && r.Acquired {
				slog.InfoContext(ctx, "source tier selected", "entry_group", entry, "source_group", id, "tier", n+1, "model", model, "account_id", r.Account.ID)
				return r, d, nil
			}
			if pending == nil && r != nil && r.WaitPlan != nil {
				pending = r
				pd = d
			}
		}
	}
	if pending != nil {
		return pending, pd, nil
	}
	return nil, pd, ErrNoAvailableAccounts
}

type SourceTierHealthRow struct {
	Index     int              `json:"index"`
	Total     int              `json:"total"`
	Available int              `json:"available"`
	Busy      int              `json:"busy"`
	Blocked   int              `json:"blocked"`
	Accounts  []map[string]any `json:"accounts"`
}

func (s *OpenAIGatewayService) SourceTierHealth(ctx context.Context, v SourceTierConfig, model string) (map[string]any, error) {
	if err := validateSourceTierConfig(v); err != nil {
		return nil, err
	}
	if s.sourceTiers == nil {
		return nil, fmt.Errorf("存储未初始化")
	}
	quality, err := s.sourceTierQualities(ctx, model)
	if err != nil {
		return nil, err
	}
	rows := []SourceTierHealthRow{}
	unclassified := map[int64]bool{}
	for n, l := range v.Layers {
		row := SourceTierHealthRow{Index: n, Accounts: []map[string]any{}}
		seen := map[int64]bool{}
		for _, g := range v.groups(l.Source) {
			rs, e := s.sourceTiers.db.QueryContext(ctx, "SELECT a.id FROM accounts a JOIN account_groups ag ON ag.account_id=a.id WHERE ag.group_id=$1 AND a.deleted_at IS NULL AND a.platform='openai' AND EXISTS(SELECT 1 FROM groups g WHERE g.id=ag.group_id AND g.status='active' AND g.deleted_at IS NULL)", g)
			if e != nil {
				return nil, e
			}
			ids := []int64{}
			for rs.Next() {
				var id int64
				if e = rs.Scan(&id); e != nil {
					rs.Close()
					return nil, e
				}
				ids = append(ids, id)
			}
			e = rs.Err()
			rs.Close()
			if e != nil {
				return nil, e
			}
			accounts, e := s.accountRepo.GetByIDs(ctx, ids)
			if e != nil {
				return nil, e
			}
			for _, a := range accounts {
				if seen[a.ID] {
					continue
				}
				seen[a.ID] = true
				verdict := sourceTierVerdict(quality[a.ID], v.MaxAgeHours, time.Now())
				if verdict == "unknown" {
					unclassified[a.ID] = true
				}
				if verdict != l.Quality {
					continue
				}
				row.Total++
				reason := "ready"
				for _, id := range a.GroupIDs {
					if slices.Contains(v.SelfGroups, id) && l.Source != "self" || slices.Contains(v.ExternalGroups, id) && l.Source != "external" {
						reason = "source_conflict"
					}
				}
				if reason == "ready" && !a.IsSchedulable() {
					reason = "disabled_or_cooling"
				}
				if reason == "ready" && s.isOpenAIAccountRequestRuntimeBlockedContext(ctx, a, model, false) {
					reason = "runtime_blocked"
				}
				ds := &defaultOpenAIAccountScheduler{service: s}
				if reason == "ready" && !ds.isAccountRequestCompatible(ctx, a, OpenAIAccountScheduleRequest{Platform: PlatformOpenAI, RequestedModel: model}) {
					reason = "model_incompatible"
				}
				if reason == "ready" && s.concurrencyService != nil {
					loads, loadErr := s.concurrencyService.GetAccountsLoadBatch(ctx, []AccountWithConcurrency{{ID: a.ID, MaxConcurrency: a.EffectiveLoadFactor()}})
					if loadErr != nil {
						reason = "load_unknown"
					} else if load := loads[a.ID]; load != nil && load.LoadRate >= 100 {
						reason = "busy"
					}
				}
				if reason == "ready" {
					row.Available++
				} else if reason == "busy" {
					row.Busy++
				} else {
					row.Blocked++
				}
				row.Accounts = append(row.Accounts, map[string]any{"id": a.ID, "name": a.Name, "reason": reason, "checked_at": quality[a.ID].At})
			}
		}
		rows = append(rows, row)
	}
	return map[string]any{"layers": rows, "unknown_accounts": len(unclassified), "checked_at": time.Now(), "model": model, "note": "可用为账号状态、模型兼容及当前实例熔断检查；实际调度还会检查并发、传输、预算和请求权限。"}, nil
}
