package service

import (
	"context"
	"encoding/json"
	"github.com/DATA-DOG/go-sqlmock"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestVMQuotaPercent(t *testing.T) {
	v := 0.23
	if *vmQuotaPercent(&v) != 23 || vmQuotaPercent(nil) != nil {
		t.Fatal("ratio conversion")
	}
}
func TestVMQuotaBindingCacheRefreshAndFailure(t *testing.T) {
	reads, refreshes := 0, 0
	fail := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private" {
			t.Error("missing management auth")
		}
		switch r.URL.Path {
		case "/api/panel/vms":
			reads++
			w.Write([]byte(`{"ok":true,"data":{"items":[{"id":"slot-1","platform":"openai","plan_type":"plus","utilization_5h":0.7,"utilization_7d":0,"reset_5h":"2026-10-24T01:00:00Z","reset_credits":{"available_count":2},"access_token":"must-not-leak"}]}}`))
		case "/api/panel/vms/slot-1/openai-quota/refresh":
			refreshes++
			if fail {
				w.WriteHeader(401)
				w.Write([]byte(`{"secret":"must-not-leak"}`))
				return
			}
			w.Write([]byte(`{"ok":true,"data":{"plan_type":"plus","quota":{"utilization_5h":0.8,"utilization_7d":0.1},"reset_credits":{"available_count":1}}}`))
		default:
			t.Errorf("unexpected operation: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	db, mock, _ := sqlmock.New()
	defer db.Close()
	s := &VM2APIBridge{db: db, base: server.URL, master: "private", client: server.Client()}
	query := func(force bool) VMQuotaStatus {
		mock.ExpectQuery("SELECT COALESCE").WithArgs(int64(42)).WillReturnRows(sqlmock.NewRows([]string{"vm"}).AddRow("slot-1"))
		v, e := s.Quota(context.Background(), 42, force)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	v := query(false)
	if *v.FiveHour.Utilization != 70 || *v.SevenDay.Utilization != 0 || *v.ResetCredits.AvailableCount != 2 {
		t.Fatalf("bad snapshot %+v", v)
	}
	query(false)
	if reads != 1 {
		t.Fatal("list not cached")
	}
	v = query(true)
	if *v.FiveHour.Utilization != 80 || v.RefreshedAt == nil || *v.ResetCredits.AvailableCount != 1 {
		t.Fatalf("bad refresh %+v", v)
	}
	query(true)
	if refreshes != 1 {
		t.Fatal("duplicate refresh not throttled")
	}
	s.quotaMu.Lock()
	s.quotaAttempts = nil
	s.quotaMu.Unlock()
	fail = true
	v = query(true)
	if !v.Stale || v.Error == "" || *v.FiveHour.Utilization != 80 {
		t.Fatalf("failed refresh lost last good values %+v", v)
	}
	raw, _ := json.Marshal(v)
	if strings.Contains(string(raw), "must-not-leak") || strings.Contains(string(raw), "private") {
		t.Fatal("secret exposed")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestVMQuotaRejectsUnsafeBindingAndMissingData(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	s := &VM2APIBridge{db: db, quotaUntil: time.Now().Add(time.Minute), quotaItems: map[string]VMQuotaStatus{}}
	mock.ExpectQuery("SELECT COALESCE").WithArgs(int64(2)).WillReturnRows(sqlmock.NewRows([]string{"vm"}).AddRow("../credentials"))
	if _, err := s.Quota(context.Background(), 2, true); err == nil {
		t.Fatal("unsafe binding accepted")
	}
	st := s.cachedQuota(context.Background(), "deleted-slot")
	if !st.Stale || st.Error == "" || st.FiveHour.Utilization != nil || st.ResetCredits != nil {
		t.Fatal("missing slot shown as zero quota")
	}
}
