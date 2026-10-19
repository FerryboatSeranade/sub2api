//go:build unit

package service

import (
	"context"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReasoningPolicyPreservesManualHealth(t *testing.T) {
	a := &Account{Status: StatusActive, Schedulable: true, Extra: map[string]any{}}
	require.True(t, a.IsSchedulable())
	a.Extra[ReasoningLatestKey] = map[string]any{"verdict": "degraded"}
	require.True(t, a.IsSchedulable(), "default is opt-in")
	a.Extra[ReasoningAutoExcludeKey] = true
	require.False(t, a.IsSchedulable())
	require.True(t, a.Schedulable, "manual state unchanged")
	for _, verdict := range []string{"not_degraded", "inconclusive", "untested"} {
		a.Extra[ReasoningLatestKey] = map[string]any{"verdict": verdict}
		require.True(t, a.IsSchedulable())
	}
	a.Schedulable = false
	require.False(t, a.IsSchedulable(), "passing probe cannot override manual pause")
	a.Schedulable = true
	reset := time.Now().Add(time.Hour)
	a.RateLimitResetAt = &reset
	require.False(t, a.IsSchedulable(), "passing probe cannot override rate limit")
}
func TestScheduledDeploymentOwnership(t *testing.T) {
	t.Setenv("SUB2API_DEPLOYMENT_SLOT", "green")
	file := filepath.Join(t.TempDir(), "owner")
	t.Setenv("SUB2API_SCHEDULER_OWNER_FILE", file)
	require.False(t, scheduledDeploymentActive())
	require.NoError(t, os.WriteFile(file, []byte("blue\n"), 0600))
	require.False(t, scheduledDeploymentActive())
	require.NoError(t, os.WriteFile(file, []byte("green\n"), 0600))
	require.True(t, scheduledDeploymentActive())
}

func TestScheduledAccountLockContentionAndRelease(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery("SELECT pg_try_advisory_lock").WithArgs(int64(7319000000000047)).WillReturnRows(sqlmock.NewRows([]string{"locked"}).AddRow(false))
	_, ok, err := lockScheduledAccount(context.Background(), db, 47)
	require.NoError(t, err)
	require.False(t, ok)
	mock.ExpectQuery("SELECT pg_try_advisory_lock").WithArgs(int64(7319000000000047)).WillReturnRows(sqlmock.NewRows([]string{"locked"}).AddRow(true))
	release, ok, err := lockScheduledAccount(context.Background(), db, 47)
	require.NoError(t, err)
	require.True(t, ok)
	mock.ExpectExec("SELECT pg_advisory_unlock").WithArgs(int64(7319000000000047)).WillReturnResult(sqlmock.NewResult(0, 1))
	release()
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestReasoningPolicyGlobalOverrides(t *testing.T) {
	for _, tc := range []struct {
		mode         string
		global, want bool
	}{
		{"inherit", false, false}, {"inherit", true, true}, {"enabled", false, true}, {"enabled", true, true}, {"disabled", true, false}, {"disabled", false, false},
	} {
		require.Equal(t, tc.want, reasoningPolicyEnabled(tc.mode, tc.global))
	}
}

func TestReasoningPolicyTransactionRollback(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	svc := &ScheduledTestService{db: db}
	mock.ExpectBegin()
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT COALESCE").WithArgs(ReasoningGlobalKey).WillReturnRows(sqlmock.NewRows([]string{"enabled"}).AddRow(true))
	mock.ExpectExec("UPDATE accounts SET extra").WithArgs("disabled", int64(18)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE accounts SET extra").WithArgs(true, int64(18)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO scheduler_outbox").WithArgs(int64(18)).WillReturnError(context.Canceled)
	mock.ExpectRollback()
	require.ErrorIs(t, svc.updateReasoningPolicy(context.Background(), 18, "disabled", nil, nil), context.Canceled)
	require.NoError(t, mock.ExpectationsWereMet())
}
