//go:build unit

package service

import (
	"context"
	"github.com/DATA-DOG/go-sqlmock"
	"testing"
)

func TestProbeFailureClassification(t *testing.T) {
	cases := []struct {
		message, code string
		permanent     bool
	}{
		{`API returned 403: {"code":"GROUP_DELETED"}`, "group_deleted", true},
		{`API returned 401: token_revoked invalidated oauth token`, "auth_invalid", true},
		{`API returned 403: This request was blocked by our usage policy.`, "policy_blocked", true},
		{`API returned 503: quota_exhausted OpenAI 号池额度已耗尽`, "quota_exhausted", false},
		{`Our servers are currently overloaded. Please try again later.`, "overloaded", false},
		{`API returned 500: server_error`, "upstream_error", false},
		{`An error occurred while processing your request. Request ID abc`, "upstream_error", false},
		{`API returned 429: rate_limit`, "rate_limited", false},
		{`context deadline exceeded`, "network", false},
	}
	for _, c := range cases {
		t.Run(c.code, func(t *testing.T) {
			f := ClassifyProbeFailure(&ScheduledTestResult{Status: "failed", ErrorMessage: c.message})
			if f.Code != c.code || f.Permanent != c.permanent {
				t.Fatalf("got %+v", f)
			}
			if !f.Permanent && f.DelaySeconds < 300 {
				t.Fatal("must not immediately retry")
			}
		})
	}
	if ClassifyProbeFailure(nil) != nil {
		t.Fatal("nil is untested")
	}
	if ClassifyProbeFailure(&ScheduledTestResult{Status: "success", ResponseText: "403 is a number"}) != nil {
		t.Fatal("successful answer is not an HTTP failure")
	}
}

func TestProbeGateHonorsExclusionAndPause(t *testing.T) {
	for _, tc := range []struct {
		name, control               string
		vm, global, manual, blocked bool
	}{
		{"explicit exclusion overrides manual", `{"excluded":true,"note":"VM redundant"}`, false, false, true, true},
		{"global VM exclusion", `{}`, true, true, true, true},
		{"permanent pause blocks schedule", `{"pause_code":"auth_invalid"}`, false, false, false, true},
		{"explicit manual validation can retry pause", `{"pause_code":"auth_invalid"}`, false, false, true, false},
		{"active normal account", `{}`, false, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, m, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			s := &ScheduledTestService{db: db}
			m.ExpectQuery("SELECT COALESCE\\(extra").WithArgs(int64(1)).WillReturnRows(sqlmock.NewRows([]string{"control", "derived"}).AddRow(tc.control, tc.vm))
			config := `{"concurrency":10,"exclude_vm":false}`
			if tc.global {
				config = `{"concurrency":10,"exclude_vm":true}`
			}
			m.ExpectQuery("SELECT COALESCE").WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(config))
			err = s.ProbeGate(context.Background(), 1, tc.manual)
			if (err != nil) != tc.blocked {
				t.Fatalf("gate=%v", err)
			}
			if err = m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
