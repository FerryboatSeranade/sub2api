package service

// VM2APIBridge provisions one isolated Codex slot and one scoped key per upstream
// identity. Only server configuration can select the trusted management endpoint.
import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

const vmBridgeLock int64 = 7318999999999998

var errVMBridgeBusy = errors.New("真实 API 测试暂时限流或并发已满，请稍后重试")

var errVMBridgeCommitUnknown = errors.New("切换提交结果未确认，请刷新进度后核对状态")

type VMBridgeState struct {
	Status        string    `json:"status"`
	Stage         string    `json:"stage"`
	Message       string    `json:"message"`
	VMID          string    `json:"vm_id,omitempty"`
	KeyID         string    `json:"key_id,omitempty"`
	TargetID      int64     `json:"target_id,omitempty"`
	StartedAt     time.Time `json:"started_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	InheritGroups bool      `json:"inherit_groups"`
	// No credentials are persisted in job state. Recovery snapshots only contain scopes.
	Cleanup []vmBridgeKey `json:"cleanup,omitempty"`
}
type vmBridgeKey struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Status         string   `json:"status"`
	GroupType      string   `json:"group_type"`
	Allowed        []string `json:"allowed_vms"`
	PoolID         any      `json:"vm_pool_id"`
	MaxConcurrency int      `json:"max_concurrency"`
}
type vmBridgeVM struct {
	ID             string `json:"id"`
	OrgID          string `json:"org_uuid"`
	Platform       string `json:"platform"`
	Status         string `json:"status"`
	Schedulable    bool   `json:"schedulable"`
	MaxConcurrency int    `json:"max_concurrency"`
}
type VM2APIBridge struct {
	db                                         *sql.DB
	admin                                      AdminService
	base, apiBase, master, group, proxy, model string
	client                                     *http.Client
}

func NewVM2APIBridge(db *sql.DB, admin AdminService) *VM2APIBridge {
	return &VM2APIBridge{db: db, admin: admin, base: strings.TrimRight(os.Getenv("VM2API_MANAGEMENT_URL"), "/"), apiBase: strings.TrimRight(os.Getenv("VM2API_UPSTREAM_URL"), "/"), master: os.Getenv("VM2API_MANAGEMENT_KEY"), group: envBridge("VM2API_GROUP_NAME", "vm2api-hz"), proxy: envBridge("VM2API_PROXY_ID", "px-local"), model: envBridge("VM2API_TEST_MODEL", "gpt-6.1-sol"), client: &http.Client{Timeout: 240 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func envBridge(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
func (s *VM2APIBridge) Enabled() bool {
	for _, v := range []string{s.base, s.apiBase} {
		u, e := url.Parse(v)
		if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
			return false
		}
	}
	return s.master != "" && s.db != nil
}
func bridgeIdentity(a *Account) (string, error) {
	if a.Platform != "openai" || a.Type != "oauth" || a.ParentAccountID != nil {
		return "", errors.New("仅支持独立 OpenAI OAuth 账号")
	}
	id, _ := a.Credentials["chatgpt_account_id"].(string)
	access, _ := a.Credentials["access_token"].(string)
	refresh, _ := a.Credentials["refresh_token"].(string)
	if id == "" || access == "" || refresh == "" {
		return "", errors.New("账号缺少 account_id、access_token 或 refresh_token，请先重新认证")
	}
	return id, nil
}
func bridgeName(identity string) string {
	h := sha256.Sum256([]byte(identity))
	return "sub2api-" + hex.EncodeToString(h[:])[:20]
}

var bridgeSafeID = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func (s *VM2APIBridge) call(ctx context.Context, method, path string, body any, out any) error {
	var buf io.Reader
	if body != nil {
		b, e := json.Marshal(body)
		if e != nil {
			return errors.New("请求编码失败")
		}
		buf = bytes.NewReader(b)
	}
	r, e := http.NewRequestWithContext(ctx, method, s.base+path, buf)
	if e != nil {
		return errors.New("vm2api 地址无效")
	}
	r.Header.Set("Authorization", "Bearer "+s.master)
	r.Header.Set("Content-Type", "application/json")
	res, e := s.client.Do(r)
	if e != nil {
		return errors.New("vm2api 请求未完成；请重试同步以核对已有资源")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("vm2api 返回 HTTP %d", res.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if e != nil {
		return errors.New("vm2api 响应读取失败")
	}
	var envelope struct {
		OK *bool `json:"ok"`
	}
	if json.Unmarshal(b, &envelope) != nil || (envelope.OK != nil && !*envelope.OK) {
		return errors.New("vm2api 操作失败")
	}
	if out != nil && json.Unmarshal(b, out) != nil {
		return errors.New("vm2api 响应格式异常")
	}
	return nil
}
func (s *VM2APIBridge) save(ctx context.Context, id int64, state *VMBridgeState) error {
	state.UpdatedAt = time.Now().UTC()
	b, _ := json.Marshal(state)
	r, e := s.db.ExecContext(ctx, "UPDATE accounts SET extra=jsonb_set(COALESCE(extra,'{}'::jsonb),'{vm2api_bridge}',$1::jsonb),updated_at=now() WHERE id=$2 AND deleted_at IS NULL", string(b), id)
	if e != nil {
		return errors.New("无法保存迁移进度")
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return errors.New("原账号已不存在")
	}
	return nil
}
func (s *VM2APIBridge) State(ctx context.Context, id int64) (*VMBridgeState, error) {
	var raw []byte
	if err := s.db.QueryRowContext(ctx, "SELECT COALESCE(extra->'vm2api_bridge','{}'::jsonb) FROM accounts WHERE id=$1 AND deleted_at IS NULL", id).Scan(&raw); err != nil {
		return nil, errors.New("账号不存在")
	}
	st := &VMBridgeState{}
	if json.Unmarshal(raw, st) != nil {
		return nil, errors.New("迁移进度无法读取")
	}
	if st.Status == "running" && time.Since(st.UpdatedAt) > 16*time.Minute {
		st.Status = "interrupted"
		st.Message = "任务已中断，可重新同步核对已有资源"
	}
	return st, nil
}

// One session advisory lock serializes provisioning across both blue/green slots.
// The lock is released by PostgreSQL even when the process exits unexpectedly.
func (s *VM2APIBridge) Start(ctx context.Context, id int64, inherit bool) (*VMBridgeState, error) {
	if !s.Enabled() {
		return nil, errors.New("vm2api 集成尚未配置")
	}
	conn, e := s.db.Conn(ctx)
	if e != nil {
		return nil, errors.New("数据库连接失败")
	}
	var locked bool
	if e = conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", vmBridgeLock).Scan(&locked); e != nil || !locked {
		conn.Close()
		return nil, errors.New("另一个 vm2api 同步任务正在进行，请稍后重试")
	}
	release := func() {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		conn.ExecContext(c, "SELECT pg_advisory_unlock($1)", vmBridgeLock)
		conn.Close()
	}
	a, e := s.admin.GetAccount(ctx, id)
	if e != nil {
		release()
		return nil, errors.New("账号不存在")
	}
	if _, e = bridgeIdentity(a); e != nil {
		release()
		return nil, e
	}
	st, e := s.State(ctx, id)
	if e != nil {
		release()
		return nil, e
	}
	st.Status = "running"
	st.Stage = "queued"
	st.Message = "准备同步"
	st.StartedAt = time.Now().UTC()
	st.InheritGroups = inherit
	if e = s.save(ctx, id, st); e != nil {
		release()
		return nil, e
	}
	result := *st
	result.Cleanup = nil
	go func() {
		defer release()
		job, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		err := s.run(job, a, st)
		if err != nil {
			recovery, stop := context.WithTimeout(context.Background(), 30*time.Second)
			defer stop()
			if persisted, readErr := s.State(recovery, id); readErr == nil && persisted.Status == "completed" && persisted.StartedAt.Equal(st.StartedAt) {
				return
			}
			if errors.Is(err, errVMBridgeCommitUnknown) {
				return
			}
			rollbackErr := s.restoreScopes(recovery, st)
			st.Status = "failed"
			st.Message = err.Error()
			if rollbackErr != nil {
				st.Message += "；旧 Key 范围恢复失败，请重试同步或检查 vm2api"
			}
			_ = s.save(recovery, id, st)
		}
	}()
	return &result, nil
}
func (s *VM2APIBridge) step(ctx context.Context, id int64, st *VMBridgeState, stage, msg string) error {
	st.Stage = stage
	st.Message = msg
	return s.save(ctx, id, st)
}
func (s *VM2APIBridge) restoreScopes(ctx context.Context, st *VMBridgeState) error {
	for _, k := range st.Cleanup {
		if !bridgeSafeID.MatchString(k.ID) {
			return errors.New("密钥标识异常")
		}
		if e := s.call(ctx, "PATCH", "/api/panel/api-keys/"+k.ID, map[string]any{"group_type": k.GroupType, "allowed_vms": k.Allowed, "vm_pool_id": k.PoolID, "status": k.Status}, nil); e != nil {
			return e
		}
	}
	st.Cleanup = nil
	return nil
}
func (s *VM2APIBridge) run(ctx context.Context, a *Account, st *VMBridgeState) error {
	identity, _ := bridgeIdentity(a)
	stable := bridgeName(identity)
	if e := s.restoreScopes(ctx, st); e != nil {
		return e
	}
	if e := s.step(ctx, a.ID, st, "container", "检查或创建专属容器"); e != nil {
		return e
	}
	var vms struct {
		Data struct {
			Items []vmBridgeVM `json:"items"`
		} `json:"data"`
	}
	if e := s.call(ctx, "GET", "/api/panel/vms", nil, &vms); e != nil {
		return e
	}
	vmID := stable
	found := false
	matches := 0
	running := false
	for _, v := range vms.Data.Items {
		if v.OrgID == identity || v.ID == stable || (st.VMID != "" && v.ID == st.VMID) {
			if v.Platform != "openai" || (v.OrgID != "" && v.OrgID != identity) {
				return errors.New("已有容器身份不匹配，未覆盖凭证")
			}
			matches++
			vmID = v.ID
			found = true
			running = v.Status == "running"
		}
	}
	if matches > 1 {
		return errors.New("发现多个同账号容器，请先在 vm2api 合并后重试")
	}
	if !bridgeSafeID.MatchString(vmID) {
		return errors.New("容器标识无效")
	}
	st.VMID = vmID
	if e := s.save(ctx, a.ID, st); e != nil {
		return e
	}
	if !found {
		if e := s.call(ctx, "POST", "/api/panel/vms/create", map[string]any{"id": vmID, "name": a.Name, "platform": "openai", "family": "codex", "kernel": "ubuntu", "runtime_type": "docker", "machine": map[string]any{"memory": "2g"}, "proxy_id": s.proxy, "start": false, "activate": false, "max_concurrency": 2}, nil); e != nil {
			return e
		}
	}
	if e := s.call(ctx, "PATCH", "/api/panel/vms/"+vmID, map[string]any{"max_concurrency": 2}, nil); e != nil {
		return e
	}
	if e := s.step(ctx, a.ID, st, "credentials", "导入最新凭证并启动容器"); e != nil {
		return e
	}
	auth, _ := json.Marshal(map[string]any{"tokens": map[string]any{"access_token": a.Credentials["access_token"], "refresh_token": a.Credentials["refresh_token"], "id_token": a.Credentials["id_token"], "account_id": identity}})
	if e := s.call(ctx, "POST", "/api/panel/vms/import", map[string]any{"vm_id": vmID, "type": "codex", "mode": "json", "auth_json": string(auth)}, nil); e != nil {
		return e
	}
	if !running {
		if e := s.call(ctx, "POST", "/api/panel/vms/"+vmID+"/start", map[string]any{}, nil); e != nil {
			return e
		}
	}
	if e := s.call(ctx, "POST", "/api/panel/vms/"+vmID+"/schedulable", map[string]any{"schedulable": true}, nil); e != nil {
		return e
	}
	if e := s.step(ctx, a.ID, st, "key", "配置仅允许该容器的专属 Key（并发 2）"); e != nil {
		return e
	}
	var keys struct {
		Keys []vmBridgeKey `json:"keys"`
	}
	if e := s.call(ctx, "GET", "/api/panel/api-keys", nil, &keys); e != nil {
		return e
	}
	keyID := ""
	for _, k := range keys.Keys {
		if k.Name == stable {
			if keyID != "" {
				return errors.New("存在重名专属 Key，请先整理")
			}
			keyID = k.ID
		}
	}
	var key string
	scope := map[string]any{"name": stable, "group_type": "openai", "allowed_vms": []string{vmID}, "vm_pool_id": nil, "max_concurrency": 2, "status": "active"}
	if keyID == "" {
		var r struct {
			Item struct {
				ID  string `json:"id"`
				Key string `json:"key"`
			} `json:"item"`
		}
		if e := s.call(ctx, "POST", "/api/panel/api-keys", scope, &r); e != nil {
			return e
		}
		keyID = r.Item.ID
		key = r.Item.Key
	} else {
		if !bridgeSafeID.MatchString(keyID) {
			return errors.New("密钥标识无效")
		}
		if e := s.call(ctx, "PATCH", "/api/panel/api-keys/"+keyID, scope, nil); e != nil {
			return e
		}
		var r struct {
			Key string `json:"key"`
		}
		if e := s.call(ctx, "POST", "/api/panel/api-keys/"+keyID+"/reveal", map[string]any{}, &r); e != nil {
			return e
		}
		key = r.Key
	}
	if key == "" || keyID == "" {
		return errors.New("专属 Key 创建结果无效")
	}
	st.KeyID = keyID
	if e := s.step(ctx, a.ID, st, "testing", "使用专属 Key 进行真实 API 冒烟测试"); e != nil {
		return e
	}
	if e := s.smoke(ctx, key); e != nil {
		return e
	}
	// Re-read the scope: a successful response alone cannot prove slot isolation.
	var verified struct {
		Keys []vmBridgeKey `json:"keys"`
	}
	if e := s.call(ctx, "GET", "/api/panel/api-keys", nil, &verified); e != nil {
		return e
	}
	valid := false
	for _, k := range verified.Keys {
		if k.ID == keyID {
			valid = k.GroupType == "openai" && len(k.Allowed) == 1 && k.Allowed[0] == vmID && k.PoolID == nil && k.MaxConcurrency == 2 && k.Status == "active"
		}
	}
	if !valid {
		return errors.New("专属 Key 范围或并发校验失败")
	}
	// Refresh after creation: unrestricted legacy keys also include newly created slots.
	if e := s.call(ctx, "GET", "/api/panel/vms", nil, &vms); e != nil {
		return e
	}
	validVM := false
	for _, v := range vms.Data.Items {
		if v.ID == vmID {
			validVM = v.MaxConcurrency == 2 && v.Status == "running" && v.OrgID == identity
		}
	}
	if !validVM {
		return errors.New("容器身份、状态或并发校验失败")
	}
	if e := s.step(ctx, a.ID, st, "cutover", "测试通过，收窄共享入口并切换账号"); e != nil {
		return e
	}
	// Only alter keys actually referenced by Sub2API accounts. Unrelated consumers
	// are not modified. Pool-backed scopes are refused rather than broadened.
	rows, e := s.db.QueryContext(ctx, "SELECT id,credentials->>'api_key' FROM accounts WHERE platform='openai' AND type='apikey' AND deleted_at IS NULL AND regexp_replace(rtrim(credentials->>'base_url','/'),'/v1$','')=$1", s.apiBase)
	if e != nil {
		return errors.New("无法检查旧共享入口")
	}
	used := map[string][]int64{}
	for rows.Next() {
		var id int64
		var secret string
		if e = rows.Scan(&id, &secret); e != nil {
			rows.Close()
			return errors.New("无法检查旧共享入口")
		}
		used[secret] = append(used[secret], id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return errors.New("无法检查旧共享入口")
	}
	var disableIDs []int64
	for _, k := range verified.Keys {
		if k.ID == keyID || k.Status != "active" {
			continue
		}
		remaining, affected := bridgeRemaining(k, vmID, vms.Data.Items)
		if !affected {
			continue
		}
		var revealed struct {
			Key string `json:"key"`
		}
		if !bridgeSafeID.MatchString(k.ID) {
			return errors.New("密钥标识无效")
		}
		if e = s.call(ctx, "POST", "/api/panel/api-keys/"+k.ID+"/reveal", map[string]any{}, &revealed); e != nil {
			return e
		}
		oldAccount, ours := used[revealed.Key]
		if !ours {
			continue
		}
		if k.PoolID != nil {
			return errors.New("旧共享 Key 绑定账号池，请先解除池绑定后迁移")
		}
		st.Cleanup = append(st.Cleanup, k)
		if e = s.save(ctx, a.ID, st); e != nil {
			return e
		}
		change := map[string]any{"group_type": "openai", "allowed_vms": remaining, "vm_pool_id": nil}
		if len(remaining) == 0 {
			change = map[string]any{"status": "disabled"}
			disableIDs = append(disableIDs, oldAccount...)
		}
		if e = s.call(ctx, "PATCH", "/api/panel/api-keys/"+k.ID, change, nil); e != nil {
			return e
		}
	}
	return s.commit(ctx, a, st, key, identity, disableIDs)
}

// Empty allowlists mean unrestricted, never write one when removing the last VM.
func bridgeRemaining(k vmBridgeKey, vm string, vms []vmBridgeVM) ([]string, bool) {
	allowed := k.Allowed
	if k.PoolID != nil {
		return nil, true
	}
	if len(allowed) == 0 && (k.GroupType == "openai" || k.GroupType == "all" || k.GroupType == "") {
		for _, v := range vms {
			if v.Platform == "openai" {
				allowed = append(allowed, v.ID)
			}
		}
	}
	out := []string{}
	found := false
	for _, id := range allowed {
		if id == vm {
			found = true
		} else {
			out = append(out, id)
		}
	}
	return out, found
}
func (s *VM2APIBridge) smoke(ctx context.Context, key string) error {
	for attempt := 0; attempt < 3; attempt++ {
		err := s.smokeOnce(ctx, key)
		if !errors.Is(err, errVMBridgeBusy) || attempt == 2 {
			return err
		}
		timer := time.NewTimer(time.Duration(attempt+1) * 5 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return errVMBridgeBusy
}
func (s *VM2APIBridge) smokeOnce(ctx context.Context, key string) error {
	c, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()
	b, _ := json.Marshal(map[string]any{"model": s.model, "input": "Reply with exactly OK.", "stream": true, "max_output_tokens": 64})
	r, _ := http.NewRequestWithContext(c, "POST", s.apiBase+"/v1/responses", bytes.NewReader(b))
	r.Header.Set("Authorization", "Bearer "+key)
	r.Header.Set("Content-Type", "application/json")
	res, e := s.client.Do(r)
	if e != nil {
		return errors.New("真实 API 测试连接失败，原账号未关闭")
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusTooManyRequests {
		return errVMBridgeBusy
	}
	if res.StatusCode != 200 {
		return fmt.Errorf("真实 API 测试返回 HTTP %d，未执行账号切换", res.StatusCode)
	}
	scan := bufio.NewScanner(io.LimitReader(res.Body, 4<<20))
	scan.Buffer(make([]byte, 4096), 1<<20)
	complete := false
	text := ""
	for scan.Scan() {
		line := scan.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var d struct {
			Type  string `json:"type"`
			Delta string `json:"delta"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &d) != nil {
			continue
		}
		if d.Type == "response.output_text.delta" {
			text += d.Delta
		}
		if d.Type == "response.completed" {
			complete = true
		}
	}
	if scan.Err() != nil || !complete || strings.TrimSpace(text) == "" {
		return errors.New("真实 API 测试未收到完整文本响应，原账号未关闭")
	}
	return nil
}
func (s *VM2APIBridge) commit(ctx context.Context, a *Account, st *VMBridgeState, key, identity string, disableIDs []int64) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return errors.New("切换事务启动失败")
	}
	defer tx.Rollback()
	var creds []byte
	if e = tx.QueryRowContext(ctx, "SELECT credentials FROM accounts WHERE id=$1 AND deleted_at IS NULL FOR UPDATE", a.ID).Scan(&creds); e != nil {
		return errors.New("原账号已不存在")
	}
	var current map[string]any
	_ = json.Unmarshal(creds, &current)
	currentAccess, _ := current["access_token"].(string)
	currentIdentity, _ := current["chatgpt_account_id"].(string)
	if currentAccess != a.Credentials["access_token"] || currentIdentity != identity {
		return errors.New("同步期间原账号凭证已变化，请重新同步")
	}
	var groupID int64
	if e = tx.QueryRowContext(ctx, "SELECT id FROM groups WHERE name=$1 AND platform='openai' AND status='active' AND deleted_at IS NULL", s.group).Scan(&groupID); e != nil {
		return errors.New("vm2api 目标分组不存在或未启用")
	}
	marker := bridgeName(identity)
	var target int64
	e = tx.QueryRowContext(ctx, "SELECT id FROM accounts WHERE extra->>'vm2api_identity'=$1 AND deleted_at IS NULL FOR UPDATE", marker).Scan(&target)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return errors.New("读取目标账号失败")
	}
	credentials, _ := json.Marshal(map[string]any{"api_key": key, "base_url": s.apiBase})
	extra, _ := json.Marshal(map[string]any{"vm2api_identity": marker, "vm2api_source_id": a.ID, "vm2api_vm_id": st.VMID, "vm2api_key_id": st.KeyID})
	if target == 0 {
		e = tx.QueryRowContext(ctx, `INSERT INTO accounts (name,platform,type,credentials,extra,concurrency,priority,rate_multiplier,status,schedulable,notes) SELECT $1,'openai','apikey',$2::jsonb,$3::jsonb,2,priority,rate_multiplier,'active',true,'Managed by vm2api bridge' FROM accounts WHERE id=$4 RETURNING id`, a.Name+" · vm2api", string(credentials), string(extra), a.ID).Scan(&target)
	} else {
		_, e = tx.ExecContext(ctx, "UPDATE accounts SET credentials=credentials || $1::jsonb,extra=extra || $2::jsonb,concurrency=2,status='active',schedulable=true,updated_at=now() WHERE id=$3 AND type='apikey'", string(credentials), string(extra), target)
	}
	if e != nil {
		return errors.New("保存目标账号失败")
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO account_groups(account_id,group_id) VALUES($1,$2) ON CONFLICT DO NOTHING", target, groupID); e != nil {
		return errors.New("绑定 vm2api 分组失败")
	}
	if st.InheritGroups {
		if _, e = tx.ExecContext(ctx, "INSERT INTO account_groups(account_id,group_id,priority) SELECT $1,ag.group_id,ag.priority FROM account_groups ag JOIN groups g ON g.id=ag.group_id WHERE ag.account_id=$2 AND g.deleted_at IS NULL AND g.platform='openai' ON CONFLICT DO NOTHING", target, a.ID); e != nil {
			return errors.New("继承分组失败")
		}
	}
	if e = migrateVMBridgePlans(ctx, tx, a.ID, target); e != nil {
		return e
	}
	// A single commit disables the original and enables the replacement; scheduler
	// cache invalidation is durable in the same transaction.
	if _, e = tx.ExecContext(ctx, "UPDATE accounts SET status='inactive',schedulable=false,concurrency=2,updated_at=now() WHERE id=$1", a.ID); e != nil {
		return errors.New("关闭原账号失败")
	}
	for _, id := range disableIDs {
		if _, e = tx.ExecContext(ctx, "UPDATE accounts SET status='inactive',schedulable=false,updated_at=now() WHERE id=$1", id); e != nil {
			return errors.New("关闭已清空的共享入口失败")
		}
	}
	completed := *st
	completed.Status = "completed"
	completed.Stage = "completed"
	completed.Message = "同步成功，原账号已关闭；单账号并发 2"
	completed.TargetID = target
	completed.Cleanup = nil
	completed.UpdatedAt = time.Now().UTC()
	raw, _ := json.Marshal(completed)
	if _, e = tx.ExecContext(ctx, "UPDATE accounts SET extra=jsonb_set(extra,'{vm2api_bridge}',$1::jsonb) WHERE id=$2", string(raw), a.ID); e != nil {
		return errors.New("保存最终结果失败")
	}
	ids := append([]int64{a.ID, target}, disableIDs...)
	for _, id := range ids {
		if _, e = tx.ExecContext(ctx, "INSERT INTO scheduler_outbox(event_type,account_id,payload) VALUES('account_changed',$1,'{}'::jsonb)", id); e != nil {
			return errors.New("调度缓存通知失败")
		}
	}
	if e = tx.Commit(); e != nil {
		return errVMBridgeCommitUnknown
	}
	*st = completed
	return nil
}

// Move plans in the cutover transaction, preserving IDs, history and enabled flags.
// Shared advisory locks prevent moving a plan while its original probe is running.
func migrateVMBridgePlans(ctx context.Context, tx *sql.Tx, source, target int64) error {
	for _, id := range []int64{source, target} {
		var acquired bool
		if err := tx.QueryRowContext(ctx, "SELECT pg_try_advisory_xact_lock($1)", int64(7319000000000000)+id).Scan(&acquired); err != nil {
			return err
		}
		if !acquired {
			return errors.New("账号定时测试正在运行，请结束后重试同步；原账号未切换")
		}
	}
	_, err := tx.ExecContext(ctx, "UPDATE scheduled_test_plans SET account_id=$1,updated_at=now() WHERE account_id=$2", target, source)
	return err
}
