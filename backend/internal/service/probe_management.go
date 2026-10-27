package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type ProbeFailure struct {
	Code         string `json:"code"`
	Label        string `json:"label"`
	Advice       string `json:"advice"`
	Permanent    bool   `json:"permanent"`
	DelaySeconds int    `json:"delay_seconds"`
}

func probeHTTPStatus(message string, status int) bool {
	code := strconv.Itoa(status)
	return strings.HasPrefix(strings.TrimSpace(message), code+":") || regexp.MustCompile(`(?i)(?:api returned|http(?: status)?|status["']?\s*[:=])\s*`+code+`\b`).MatchString(message)
}

func ClassifyProbeFailure(r *ScheduledTestResult) *ProbeFailure {
	if r == nil || r.Status == "success" {
		return nil
	}
	m := strings.ToLower(r.ErrorMessage)
	f := &ProbeFailure{Code: "unknown", Label: "调用异常", Advice: "保留原始错误；延后检测，持续异常时人工检查", DelaySeconds: 900}
	switch {
	case strings.Contains(m, "group_deleted"):
		f = &ProbeFailure{"group_deleted", "分组已删除", "修复上游 Key 所属分组或更换 Key 后手动恢复检测", true, 0}
	case strings.Contains(m, "token_revoked") || strings.Contains(m, "invalidated oauth") || probeHTTPStatus(m, 401):
		f = &ProbeFailure{"auth_invalid", "认证失效", "重新登录或更新凭证后手动恢复检测", true, 0}
	case strings.Contains(m, "usage policy") || probeHTTPStatus(m, 403):
		f = &ProbeFailure{"policy_blocked", "访问被阻断（403）", "检查上游策略、账号权限和模型支持；修复后手动恢复检测", true, 0}
	case strings.Contains(m, "quota_exhausted") || strings.Contains(m, "额度已耗尽"):
		f = &ProbeFailure{"quota_exhausted", "额度耗尽", "等待额度恢复或修复账号池；至少 1 小时后再检测", false, 3600}
	case strings.Contains(m, "overloaded"):
		f = &ProbeFailure{"overloaded", "上游过载", "不立即重试，至少 5 分钟后再检测", false, 300}
	case probeHTTPStatus(m, 429) || strings.Contains(m, "rate_limit"):
		f = &ProbeFailure{"rate_limited", "请求限流", "降低请求频率，至少 15 分钟后再检测", false, 900}
	case probeHTTPStatus(m, 500) || probeHTTPStatus(m, 503) || strings.Contains(m, "server_error") || strings.Contains(m, "processing your request"):
		f = &ProbeFailure{"upstream_error", "上游服务异常", "保留 request ID，至少 5 分钟后再检测", false, 300}
	case strings.Contains(m, "timeout") || strings.Contains(m, "deadline") || strings.Contains(m, "connection"):
		f = &ProbeFailure{"network", "网络或超时", "检查网络，至少 5 分钟后再检测", false, 300}
	}
	return f
}

type ProbeControl struct {
	Excluded   bool       `json:"excluded"`
	Note       string     `json:"note"`
	PauseCode  string     `json:"pause_code"`
	PauseUntil *time.Time `json:"pause_until"`
	Failures   int        `json:"failures"`
}
type ProbeReview struct {
	ResultID int64     `json:"result_id"`
	Verdict  string    `json:"verdict"`
	Note     string    `json:"note"`
	At       time.Time `json:"at"`
}
type ProbeManagementItem struct {
	AccountID   int64                `json:"account_id"`
	PlanType    string               `json:"plan_type"`
	SourceID    int64                `json:"source_id"`
	SourceName  string               `json:"source_name"`
	AccountName string               `json:"account_name"`
	AccountType string               `json:"account_type"`
	Deleted     bool                 `json:"deleted"`
	VMState     string               `json:"vm_state"`
	TargetID    int64                `json:"target_id"`
	Control     ProbeControl         `json:"control"`
	Review      *ProbeReview         `json:"review,omitempty"`
	Plans       []*ScheduledTestPlan `json:"plans"`
	Result      *ScheduledTestResult `json:"result"`
	Failure     *ProbeFailure        `json:"failure,omitempty"`
}
type ProbeSettings struct {
	Concurrency     int  `json:"concurrency"`
	ExcludeVM       bool `json:"exclude_vm"`
	ExcludeVMSource bool `json:"exclude_vm_source"`
}

func (s *ScheduledTestService) ProbeSettings(ctx context.Context) (ProbeSettings, error) {
	out := ProbeSettings{Concurrency: 10}
	if s.db == nil {
		return out, nil
	}
	var raw string
	err := s.db.QueryRowContext(ctx, "SELECT COALESCE((SELECT value FROM settings WHERE key='probe_management'), '{}')").Scan(&raw)
	if err != nil {
		return out, err
	}
	if err = json.Unmarshal([]byte(raw), &out); err != nil {
		return out, err
	}
	if out.Concurrency < 1 || out.Concurrency > 32 {
		out.Concurrency = 10
	}
	return out, nil
}
func (s *ScheduledTestService) SetProbeSettings(ctx context.Context, v ProbeSettings) error {
	if v.Concurrency < 1 || v.Concurrency > 32 {
		return fmt.Errorf("检测并发必须为 1–32")
	}
	b, _ := json.Marshal(v)
	_, err := s.db.ExecContext(ctx, "INSERT INTO settings(key,value,updated_at) VALUES('probe_management',$1,NOW()) ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value,updated_at=NOW()", string(b))
	return err
}
func (s *ScheduledTestService) ProbeManagement(ctx context.Context, kind string) ([]*ProbeManagementItem, error) {
	if kind != ScheduledTestCandyV1 && kind != ScheduledTestPelicanV1 {
		return nil, fmt.Errorf("invalid test kind")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT a.id,a.name,a.type,a.deleted_at IS NOT NULL,COALESCE(a.extra,'{}'),
 COALESCE(NULLIF(src.credentials->>'plan_type',''),NULLIF(a.credentials->>'plan_type',''),'unknown'),COALESCE(src.id,0),COALESCE(src.name,''),
 COALESCE((SELECT json_agg(p ORDER BY p.id) FROM scheduled_test_plans p WHERE p.account_id=a.id AND p.test_kind=$1),'[]'),
 (SELECT row_to_json(r) FROM scheduled_test_results r JOIN scheduled_test_plans p ON p.id=r.plan_id WHERE p.account_id=a.id AND r.test_kind=$1 ORDER BY r.finished_at DESC,r.id DESC LIMIT 1)
 FROM accounts a LEFT JOIN accounts src ON src.id::text=a.extra->>'vm2api_source_id' WHERE a.platform='openai' AND (a.deleted_at IS NULL OR EXISTS(SELECT 1 FROM scheduled_test_plans p WHERE p.account_id=a.id AND p.enabled)) ORDER BY a.id`, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*ProbeManagementItem{}
	for rows.Next() {
		i := &ProbeManagementItem{VMState: "not_linked"}
		var extra, plans []byte
		var result sql.NullString
		if err = rows.Scan(&i.AccountID, &i.AccountName, &i.AccountType, &i.Deleted, &extra, &i.PlanType, &i.SourceID, &i.SourceName, &plans, &result); err != nil {
			return nil, err
		}
		var e map[string]json.RawMessage
		if err = json.Unmarshal(extra, &e); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(e["probe_control"], &i.Control)
		_ = json.Unmarshal(e["probe_review_"+kind], &i.Review)
		if _, ok := e["vm2api_source_id"]; ok {
			i.VMState = "derived"
		} else {
			var st VMBridgeState
			_ = json.Unmarshal(e["vm2api_bridge"], &st)
			if st.TargetID > 0 {
				i.VMState = "linked"
				i.TargetID = st.TargetID
			}
		}
		if err = json.Unmarshal(plans, &i.Plans); err != nil {
			return nil, err
		}
		if result.Valid {
			if err = json.Unmarshal([]byte(result.String), &i.Result); err != nil {
				return nil, err
			}
			EvaluateReasoningProbe(i.Result)
			EvaluatePelicanProbe(i.Result)
		}
		if i.Review != nil && (i.Result == nil || i.Review.ResultID != i.Result.ID) {
			i.Review = nil
		}
		i.Failure = ClassifyProbeFailure(i.Result)
		out = append(out, i)
	}
	return out, rows.Err()
}

// Mutate one nested field atomically; never replace unrelated credentials or account settings.
func (s *ScheduledTestService) probeExtra(ctx context.Context, id int64, key string, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	r, err := s.db.ExecContext(ctx, `UPDATE accounts SET extra=jsonb_set(COALESCE(extra,'{}'),ARRAY[$2::text],$3::jsonb,true),updated_at=NOW() WHERE id=$1 AND deleted_at IS NULL`, id, key, string(b))
	if err != nil {
		return err
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return fmt.Errorf("账号已删除或不存在")
	}
	return nil
}
func (s *ScheduledTestService) probeControl(ctx context.Context, id int64) (ProbeControl, bool, bool, error) {
	var raw []byte
	var derived bool
	var bridgeRaw []byte
	var c ProbeControl
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(extra->'probe_control','{}'),COALESCE(extra ? 'vm2api_source_id',false),COALESCE(extra->'vm2api_bridge','{}') FROM accounts WHERE id=$1 AND deleted_at IS NULL`, id).Scan(&raw, &derived, &bridgeRaw)
	if err != nil {
		return c, false, false, err
	}
	err = json.Unmarshal(raw, &c)
	var bridge VMBridgeState
	if e := json.Unmarshal(bridgeRaw, &bridge); e != nil {
		return c, derived, false, e
	}
	return c, derived, !derived && bridge.TargetID > 0, err
}
func (s *ScheduledTestService) ProbeGate(ctx context.Context, id int64, manual bool) error {
	if s.db == nil {
		return nil
	}
	c, vm, source, err := s.probeControl(ctx, id)
	if err != nil {
		return fmt.Errorf("账号已删除或无法读取：%w", err)
	}
	settings, err := s.ProbeSettings(ctx)
	if err != nil {
		return err
	}
	if c.Excluded {
		return fmt.Errorf("已排除检测：%s（已有计划也不执行）", c.Note)
	}
	if settings.ExcludeVM && vm {
		return fmt.Errorf("已全局排除 vm2api 衍生账号检测；已有计划不执行")
	}
	if settings.ExcludeVMSource && source {
		return fmt.Errorf("已全局排除已转为 vm2api 的原生账号检测；已有计划不执行")
	}
	if !manual && c.PauseCode != "" && (c.PauseUntil == nil || c.PauseUntil.After(time.Now())) {
		return fmt.Errorf("检测暂停：%s", c.PauseCode)
	}
	return nil
}
func (s *ScheduledTestService) RecordProbeFailure(ctx context.Context, id int64, r *ScheduledTestResult) error {
	if s.db == nil {
		return nil
	}
	c, _, _, err := s.probeControl(ctx, id)
	if err != nil {
		return err
	}
	f := ClassifyProbeFailure(r)
	if f == nil {
		c.PauseCode = ""
		c.PauseUntil = nil
		c.Failures = 0
	} else {
		if c.PauseCode == f.Code {
			c.Failures++
		} else {
			c.Failures = 1
		}
		c.PauseCode = f.Code
		c.PauseUntil = nil
		if !f.Permanent {
			mult := 1 << min(c.Failures-1, 4)
			delay := min(f.DelaySeconds*mult, 21600)
			until := time.Now().Add(time.Duration(delay) * time.Second)
			c.PauseUntil = &until
		}
	}
	// Only execution fields change: preserve administrator exclusions made during a probe.
	b, _ := json.Marshal(map[string]any{"pause_code": c.PauseCode, "pause_until": c.PauseUntil, "failures": c.Failures})
	_, err = s.db.ExecContext(ctx, `UPDATE accounts SET extra=jsonb_set(COALESCE(extra,'{}'),'{probe_control}',COALESCE(extra->'probe_control','{}')||$2::jsonb,true) WHERE id=$1 AND deleted_at IS NULL`, id, string(b))
	return err
}

type ProbeAction struct {
	IDs      []int64 `json:"ids"`
	Action   string  `json:"action"`
	Kind     string  `json:"kind"`
	Model    string  `json:"model"`
	Cron     string  `json:"cron"`
	Note     string  `json:"note"`
	Verdict  string  `json:"verdict"`
	ResultID int64   `json:"result_id"`
}
type ProbeActionResult struct {
	ID    int64  `json:"id"`
	Error string `json:"error,omitempty"`
}

func (s *ScheduledTestService) ProbeAction(ctx context.Context, a ProbeAction) ([]ProbeActionResult, error) {
	if len(a.IDs) == 0 || len(a.IDs) > 500 {
		return nil, fmt.Errorf("请选择 1–500 个账号")
	}
	if a.Kind != ScheduledTestCandyV1 && a.Kind != ScheduledTestPelicanV1 {
		return nil, fmt.Errorf("invalid test kind")
	}
	if a.Action == "schedule" {
		if _, err := computeNextRun(a.Cron, time.Now()); err != nil {
			return nil, err
		}
	}
	out := []ProbeActionResult{}
	seen := map[int64]bool{}
	for _, id := range a.IDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		err := s.probeActionOne(ctx, id, a)
		r := ProbeActionResult{ID: id}
		if err != nil {
			r.Error = err.Error()
		}
		out = append(out, r)
	}
	return out, nil
}
func (s *ScheduledTestService) probeActionOne(ctx context.Context, id int64, a ProbeAction) error {
	c, _, _, err := s.probeControl(ctx, id)
	if err != nil {
		return fmt.Errorf("账号已删除或不存在")
	}
	switch a.Action {
	case "exclude", "include", "resume":
		patch := map[string]any{}
		if a.Action == "resume" {
			patch["pause_code"] = ""
			patch["pause_until"] = nil
			patch["failures"] = 0
		} else {
			patch["excluded"] = a.Action == "exclude"
			patch["note"] = a.Note
		}
		b, _ := json.Marshal(patch)
		_, err = s.db.ExecContext(ctx, `UPDATE accounts SET extra=jsonb_set(COALESCE(extra,'{}'),'{probe_control}',COALESCE(extra->'probe_control','{}')||$2::jsonb,true) WHERE id=$1 AND deleted_at IS NULL`, id, string(b))
		return err
	case "protect":
		plans, err := s.ListPlansByAccount(ctx, id)
		if err != nil {
			return err
		}
		var latest *ScheduledTestResult
		for _, p := range plans {
			rs, err := s.ListResults(ctx, p.ID, 1)
			if err != nil {
				return err
			}
			if len(rs) > 0 && (latest == nil || rs[0].FinishedAt.After(latest.FinishedAt)) {
				latest = rs[0]
			}
		}
		if latest != nil {
			return s.RecordProbeFailure(ctx, id, latest)
		}
		return nil
	case "review":
		release, ok, lockErr := lockScheduledAccount(ctx, s.db, id)
		if lockErr != nil {
			return lockErr
		}
		if !ok {
			return fmt.Errorf("账号正在检测，请稍后审核")
		}
		defer release()
		if a.Verdict != "degraded" && a.Verdict != "not_degraded" && a.Verdict != "inconclusive" && a.Verdict != "clear" {
			return fmt.Errorf("invalid verdict")
		}
		if a.Verdict == "clear" {
			return s.probeExtra(ctx, id, "probe_review_"+a.Kind, nil)
		}
		var latest int64
		err = s.db.QueryRowContext(ctx, `SELECT r.id FROM scheduled_test_results r JOIN scheduled_test_plans p ON p.id=r.plan_id WHERE p.account_id=$1 AND r.test_kind=$2 ORDER BY r.finished_at DESC,r.id DESC LIMIT 1`, id, a.Kind).Scan(&latest)
		if err != nil || latest != a.ResultID {
			return fmt.Errorf("结果已更新，请刷新后重新审核")
		}
		if strings.TrimSpace(a.Note) == "" {
			return fmt.Errorf("请填写审核依据")
		}
		return s.probeExtra(ctx, id, "probe_review_"+a.Kind, ProbeReview{latest, a.Verdict, a.Note, time.Now()})
	case "schedule", "disable":
		_ = c
		// Serialize duplicate bulk submissions for this account.
		release, ok, err := lockScheduledAccount(ctx, s.db, id)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("账号正在检测或变更，请稍后操作")
		}
		defer release()
		plans, err := s.ListPlansByAccount(ctx, id)
		if err != nil {
			return err
		}
		wanted := &ScheduledTestPlan{TestKind: a.Kind, ModelID: a.Model}
		if a.Action == "schedule" {
			if err := validateScheduledTestKind(wanted); err != nil {
				return err
			}
		}
		for _, p := range plans {
			if p.TestKind == a.Kind && (a.Action == "disable" || p.ModelID == wanted.ModelID) {
				if a.Action == "disable" {
					p.Enabled = false
				} else {
					p.Enabled = true
					p.CronExpression = a.Cron
					if a.Model != "" {
						p.ModelID = a.Model
					}
				}
				if _, err = s.UpdatePlan(ctx, p); err != nil {
					return err
				}
				if a.Action == "schedule" {
					return nil
				}
			}
		}
		if a.Action == "disable" {
			return nil
		}
		_, err = s.CreatePlan(ctx, &ScheduledTestPlan{AccountID: id, TestKind: a.Kind, ModelID: a.Model, CronExpression: a.Cron, Enabled: true, MaxResults: 50})
		return err
	default:
		return fmt.Errorf("unknown action")
	}
}
func (s *ScheduledTestService) CleanupDeletedProbePlans(ctx context.Context) (int64, error) {
	r, err := s.db.ExecContext(ctx, `UPDATE scheduled_test_plans p SET enabled=false,updated_at=NOW() WHERE enabled AND NOT EXISTS(SELECT 1 FROM accounts a WHERE a.id=p.account_id AND a.deleted_at IS NULL)`)
	if err != nil {
		return 0, err
	}
	return r.RowsAffected()
}

// Use database locks so manual and scheduled requests share one configurable cap,
// including overlapping blue/green processes. Account request concurrency stays unchanged.
func (s *ScheduledTestService) acquireProbeSlot(ctx context.Context) (func(), error) {
	if s.db == nil {
		return func() {}, nil
	}
	for {
		v, err := s.ProbeSettings(ctx)
		if err != nil {
			return nil, err
		}
		for n := 0; n < v.Concurrency; n++ {
			release, ok, err := lockScheduledAccount(ctx, s.db, -10000-int64(n))
			if err != nil {
				return nil, err
			}
			if ok {
				return release, nil
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}
