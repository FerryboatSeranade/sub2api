package service

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/DATA-DOG/go-sqlmock"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVMBridgeIdentity(t *testing.T) {
	a := &Account{Platform: "openai", Type: "oauth", Credentials: map[string]any{"chatgpt_account_id": "identity", "access_token": "secret", "refresh_token": "refresh"}}
	id, e := bridgeIdentity(a)
	if e != nil || id != "identity" {
		t.Fatal(e)
	}
	if bridgeName(id) != bridgeName(id) || bridgeName(id) == bridgeName("other") {
		t.Fatal("unstable identity")
	}
	a.Type = "apikey"
	if _, e = bridgeIdentity(a); e == nil {
		t.Fatal("apikey accepted")
	}
	a.Type = "oauth"
	delete(a.Credentials, "refresh_token")
	if _, e = bridgeIdentity(a); e == nil {
		t.Fatal("incomplete credentials accepted")
	}
}
func TestVMBridgeScopeRemoval(t *testing.T) {
	vms := []vmBridgeVM{{ID: "a", Platform: "openai"}, {ID: "b", Platform: "openai"}}
	for _, tc := range []struct {
		k        vmBridgeKey
		want     string
		affected bool
	}{
		{vmBridgeKey{Allowed: []string{"a", "b"}}, "b", true},
		{vmBridgeKey{Allowed: []string{"a"}}, "", true},
		{vmBridgeKey{Allowed: []string{"b"}}, "b", false},
		{vmBridgeKey{GroupType: "openai"}, "b", true},
		{vmBridgeKey{GroupType: "anthropic"}, "", false},
		{vmBridgeKey{PoolID: "pool"}, "", true},
	} {
		got, affected := bridgeRemaining(tc.k, "a", vms)
		if strings.Join(got, ",") != tc.want || affected != tc.affected {
			t.Fatalf("%+v => %v %v", tc.k, got, affected)
		}
	}
}
func TestVMBridgeCallRedactsErrorsAndRejectsRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		w.Write([]byte(`{"error":"refresh_token=secret"}`))
	}))
	defer server.Close()
	s := &VM2APIBridge{base: server.URL, master: "secret", client: server.Client()}
	e := s.call(context.Background(), "POST", "/api/panel/vms/import", map[string]any{}, nil)
	if e == nil || strings.Contains(e.Error(), "secret") {
		t.Fatalf("unsafe error: %v", e)
	}
}
func TestVMBridgeSmokeRequiresCompleteText(t *testing.T) {
	for _, tc := range []struct {
		body string
		ok   bool
	}{
		{"data: {\"type\":\"response.output_text.delta\",\"delta\":\"OK\"}\n\ndata: {\"type\":\"response.completed\"}\n\n", true},
		{"data: {\"type\":\"response.output_text.delta\",\"delta\":\"OK\"}\n\n", false},
		{"data: {\"type\":\"response.completed\"}\n\n", false},
		{"data: {\"type\":\"response.failed\"}\n\n", false},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer dedicated" {
				t.Error("test must use dedicated key")
			}
			if r.Header.Get("x-kin-vm") != "" {
				t.Error("must not bypass scoped routing with master pin")
			}
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if body["stream"] != true {
				t.Error("expected stream")
			}
			w.Write([]byte(tc.body))
		}))
		s := &VM2APIBridge{apiBase: server.URL, model: "test", client: server.Client()}
		e := s.smoke(context.Background(), "dedicated")
		if (e == nil) != tc.ok {
			t.Fatalf("ok=%v err=%v", tc.ok, e)
		}
		server.Close()
	}
}

func TestVMBridgeCommitCredentialRaceRollsBack(t *testing.T) {
	db, mock, e := sqlmock.New()
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	s := &VM2APIBridge{db: db}
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT credentials FROM accounts").WithArgs(int64(47)).WillReturnRows(sqlmock.NewRows([]string{"credentials"}).AddRow(`{"access_token":"new","chatgpt_account_id":"identity"}`))
	mock.ExpectRollback()
	e = s.commit(context.Background(), &Account{ID: 47, Credentials: map[string]any{"access_token": "old"}}, &VMBridgeState{}, "dedicated", "identity", nil)
	if e == nil || !strings.Contains(e.Error(), "凭证已变化") {
		t.Fatalf("race accepted: %v", e)
	}
	if e = mock.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}
func TestVMBridgeCommitAtomicSwitch(t *testing.T) {
	db, mock, e := sqlmock.New()
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	s := &VM2APIBridge{db: db, group: "vm2api-hz", apiBase: "http://internal"}
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT credentials FROM accounts").WithArgs(int64(47)).WillReturnRows(sqlmock.NewRows([]string{"credentials"}).AddRow(`{"access_token":"same","chatgpt_account_id":"identity"}`))
	mock.ExpectQuery("SELECT id FROM groups").WithArgs("vm2api-hz").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(10))
	mock.ExpectQuery("SELECT id FROM accounts").WithArgs(bridgeName("identity")).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("INSERT INTO accounts").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(90))
	mock.ExpectExec("INSERT INTO account_groups").WithArgs(int64(90), int64(10)).WillReturnResult(sqlmock.NewResult(0, 1))
	for _, id := range []int64{47, 90} {
		mock.ExpectQuery("SELECT pg_try_advisory_xact_lock").WithArgs(int64(7319000000000000) + id).WillReturnRows(sqlmock.NewRows([]string{"locked"}).AddRow(true))
	}
	mock.ExpectExec("UPDATE scheduled_test_plans SET account_id").WithArgs(int64(90), int64(47)).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec("UPDATE accounts SET status='inactive'").WithArgs(int64(47)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE accounts SET status='inactive'").WithArgs(int64(67)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE accounts SET extra=jsonb_set").WillReturnResult(sqlmock.NewResult(0, 1))
	for _, id := range []int64{47, 90, 67} {
		mock.ExpectExec("INSERT INTO scheduler_outbox").WithArgs(id).WillReturnResult(sqlmock.NewResult(0, 1))
	}
	mock.ExpectCommit()
	st := &VMBridgeState{VMID: "vm", KeyID: "key", Status: "running"}
	e = s.commit(context.Background(), &Account{ID: 47, Name: "source", Credentials: map[string]any{"access_token": "same"}}, st, "dedicated", "identity", []int64{67})
	if e != nil {
		t.Fatal(e)
	}
	if st.Status != "completed" || st.TargetID != 90 {
		t.Fatalf("bad final state: %+v", st)
	}
	if e = mock.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}

func TestVMBridgeNewSlotFailedSmokeNeverCutsOver(t *testing.T) {
	db, mock, e := sqlmock.New()
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	for i := 0; i < 5; i++ {
		mock.ExpectExec("UPDATE accounts SET extra=jsonb_set").WillReturnResult(sqlmock.NewResult(0, 1))
	}
	stable := bridgeName("new-identity")
	created, imported := false, false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/panel/vms" && r.Method == "GET":
			w.Write([]byte(`{"ok":true,"data":{"items":[]}}`))
		case r.URL.Path == "/api/panel/vms/create":
			var b map[string]any
			json.NewDecoder(r.Body).Decode(&b)
			if b["id"] != stable || b["max_concurrency"] != float64(2) {
				t.Error("incorrect isolated slot")
			}
			created = true
			w.Write([]byte(`{"ok":true}`))
		case r.URL.Path == "/api/panel/vms/import":
			imported = true
			w.Write([]byte(`{"ok":true}`))
		case r.URL.Path == "/api/panel/api-keys" && r.Method == "GET":
			w.Write([]byte(`{"ok":true,"keys":[]}`))
		case r.URL.Path == "/api/panel/api-keys" && r.Method == "POST":
			var b struct {
				Allowed []string `json:"allowed_vms"`
				Max     int      `json:"max_concurrency"`
			}
			json.NewDecoder(r.Body).Decode(&b)
			if len(b.Allowed) != 1 || b.Allowed[0] != stable || b.Max != 2 {
				t.Error("key not isolated")
			}
			w.Write([]byte(`{"ok":true,"item":{"id":"key_new","key":"dedicated"}}`))
		case r.URL.Path == "/v1/responses":
			w.WriteHeader(403)
			w.Write([]byte(`{"error":"do not leak credential"}`))
		default:
			w.Write([]byte(`{"ok":true}`))
		}
	}))
	defer server.Close()
	s := &VM2APIBridge{db: db, base: server.URL, apiBase: server.URL, client: server.Client()}
	a := &Account{ID: 1, Platform: "openai", Type: "oauth", Credentials: map[string]any{"chatgpt_account_id": "new-identity", "access_token": "secret", "refresh_token": "refresh"}}
	e = s.run(context.Background(), a, &VMBridgeState{Status: "running"})
	if e == nil || !strings.Contains(e.Error(), "403") || !created || !imported {
		t.Fatalf("unexpected run: %v created=%v imported=%v", e, created, imported)
	}
	if e = mock.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}

func TestVMBridgePlanMigration(t *testing.T) {
	for _, busy := range []bool{false, true} {
		t.Run(fmt.Sprint("busy=", busy), func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			mock.ExpectBegin()
			mock.ExpectQuery("SELECT pg_try_advisory_xact_lock").WithArgs(int64(7319000000000047)).WillReturnRows(sqlmock.NewRows([]string{"locked"}).AddRow(!busy))
			if !busy {
				mock.ExpectQuery("SELECT pg_try_advisory_xact_lock").WithArgs(int64(7319000000000068)).WillReturnRows(sqlmock.NewRows([]string{"locked"}).AddRow(true))
				mock.ExpectExec("UPDATE scheduled_test_plans SET account_id").WithArgs(int64(68), int64(47)).WillReturnResult(sqlmock.NewResult(0, 2))
			}
			mock.ExpectRollback()
			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			err = migrateVMBridgePlans(context.Background(), tx, 47, 68)
			if (err != nil) != busy {
				t.Fatalf("unexpected migration result: %v", err)
			}
			tx.Rollback()
			if err = mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

type movedVMBridgePlanRepo struct{ ScheduledTestPlanRepository }

func (movedVMBridgePlanRepo) GetByID(context.Context, int64) (*ScheduledTestPlan, error) {
	return &ScheduledTestPlan{ID: 1, AccountID: 68}, nil
}
func TestVMBridgeQueuedPlanCannotRunOnOldAccount(t *testing.T) {
	s := NewScheduledTestRunnerService(movedVMBridgePlanRepo{}, nil, nil, nil, nil)
	if cap(s.reasoningSlots) != 10 {
		t.Fatal("reasoning concurrency must be 10")
	}
	for _, scheduled := range []bool{true, false} {
		_, err := s.executePlan(context.Background(), &ScheduledTestPlan{ID: 1, AccountID: 47}, scheduled)
		if err == nil || !strings.Contains(err.Error(), "moved") {
			t.Fatalf("stale plan not rejected: %v", err)
		}
	}
}
