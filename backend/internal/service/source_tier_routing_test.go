//go:build unit

package service

import (
	"context"
	"encoding/json"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestSourceTierValidation(t *testing.T) {
	c := defaultSourceTierConfig()
	require.NoError(t, validateSourceTierConfig(c))
	c.Enabled = true
	require.Error(t, validateSourceTierConfig(c))
	c.EntryGroups = []int64{30}
	c.SelfGroups = []int64{10}
	c.ExternalGroups = []int64{20}
	require.NoError(t, validateSourceTierConfig(c))
	c.ExternalGroups = []int64{10}
	require.Error(t, validateSourceTierConfig(c))
	c.ExternalGroups = []int64{20}
	c.Layers = append(c.Layers, c.Layers[0])
	require.Error(t, validateSourceTierConfig(c))
}
func TestSourceTierQualityAge(t *testing.T) {
	now := time.Now()
	require.Equal(t, "not_degraded", sourceTierVerdict(sourceTierQuality{"not_degraded", now}, 48, now))
	require.Equal(t, "unknown", sourceTierVerdict(sourceTierQuality{"not_degraded", now.Add(-49 * time.Hour)}, 48, now))
	require.Equal(t, "unknown", sourceTierVerdict(sourceTierQuality{}, 48, now))
	require.Equal(t, "degraded", sourceTierVerdict(sourceTierQuality{"degraded", now}, 48, now))
}

type sourceTierRepoTest struct {
	AccountRepository
	rows map[int64][]Account
}

func (r sourceTierRepoTest) ListSchedulableByGroupIDAndPlatform(_ context.Context, id int64, _ string) ([]Account, error) {
	return r.rows[id], nil
}
func TestSourceTierPriorityAndFailureFallback(t *testing.T) {
	for _, tc := range []struct {
		name        string
		unavailable map[int64]bool
		excluded    map[int64]struct{}
		want        int64
		tier        int
	}{
		{"self first", nil, nil, 1, 0},
		{"external fallback", map[int64]bool{1: true}, nil, 2, 1},
		{"degraded last", map[int64]bool{1: true, 2: true}, nil, 3, 2},
		{"request retry excludes failed self", nil, map[int64]struct{}{1: {}}, 2, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, m, e := sqlmock.New()
			require.NoError(t, e)
			defer db.Close()
			c := defaultSourceTierConfig()
			c.Enabled = true
			c.EntryGroups = []int64{30}
			c.SelfGroups = []int64{10}
			c.ExternalGroups = []int64{20}
			rows := sqlmock.NewRows([]string{"id", "result"})
			for _, id := range []int64{1, 2, 3} {
				answer := "21"
				if id == 3 {
					answer = "20"
				}
				raw, _ := json.Marshal(ScheduledTestResult{ID: id, ModelID: "gpt-6.1-sol", TestKind: ScheduledTestCandyV1, Status: "success", ResponseText: answer, FinishedAt: time.Now()})
				rows.AddRow(id, raw)
			}
			m.ExpectQuery("SELECT DISTINCT ON").WithArgs("gpt-6.1-sol").WillReturnRows(rows)
			for i := 0; i <= tc.tier; i++ {
				g := int64(10)
				if i == 1 {
					g = 20
				}
				m.ExpectQuery("SELECT EXISTS").WithArgs(g).WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(true))
			}
			repo := sourceTierRepoTest{rows: map[int64][]Account{10: {{ID: 1, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true, GroupIDs: []int64{10}}, {ID: 3, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true, GroupIDs: []int64{10}}}, 20: {{ID: 2, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true, GroupIDs: []int64{20}}}}}
			svc := &OpenAIGatewayService{sourceTiers: &sourceTierStore{db: db}, accountRepo: repo}
			fn := func(_ context.Context, g *int64, session string, blocked map[int64]struct{}) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
				require.Equal(t, "tier:30:session", session)
				for _, a := range repo.rows[*g] {
					if _, no := blocked[a.ID]; !no && !tc.unavailable[a.ID] {
						copy := a
						return &AccountSelectionResult{Account: &copy, Acquired: true}, OpenAIAccountScheduleDecision{}, nil
					}
				}
				return nil, OpenAIAccountScheduleDecision{}, ErrNoAvailableAccounts
			}
			r, d, e := svc.selectSourceTiers(context.Background(), c, 30, "gpt-6.1-sol", "session", tc.excluded, false, fn)
			require.NoError(t, e)
			require.Equal(t, tc.want, r.Account.ID)
			require.Equal(t, tc.tier, d.ReasoningTier)
			require.NoError(t, m.ExpectationsWereMet())
		})
	}
}

// Reject newly appeared sticky accounts and preserve upstream-owned continuations.
func TestSourceTierSelectionBoundary(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		returned                  int64
		owned, ownerHit, conflict bool
		ok                        bool
	}{
		{"unexpected sticky account", 99, false, false, false, false},
		{"owned continuation must not move", 1, true, false, false, false},
		{"owned continuation keeps owner", 1, true, true, false, true},
		{"ambiguous source is excluded", 1, false, false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, m, e := sqlmock.New()
			require.NoError(t, e)
			defer db.Close()
			m.ExpectQuery("SELECT EXISTS").WithArgs(int64(10)).WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(true))
			c := defaultSourceTierConfig()
			c.SelfGroups = []int64{10}
			c.ExternalGroups = []int64{20}
			c.Layers = c.Layers[:1]
			a := Account{ID: 1, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true, GroupIDs: []int64{10}}
			if tc.conflict {
				a.GroupIDs = append(a.GroupIDs, 20)
			}
			svc := &OpenAIGatewayService{sourceTiers: &sourceTierStore{db: db, qualities: map[string]sourceTierQualityCache{"gpt-6.1-sol": {values: map[int64]sourceTierQuality{1: {"not_degraded", time.Now()}}, until: time.Now().Add(time.Minute)}}}, accountRepo: sourceTierRepoTest{rows: map[int64][]Account{10: {a}}}}
			released := false
			called := false
			fn := func(context.Context, *int64, string, map[int64]struct{}) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
				called = true
				copy := a
				copy.ID = tc.returned
				return &AccountSelectionResult{Account: &copy, Acquired: true, ReleaseFunc: func() { released = true }}, OpenAIAccountScheduleDecision{StickyPreviousHit: tc.ownerHit}, nil
			}
			r, _, err := svc.selectSourceTiers(context.Background(), c, 30, "gpt-6.1-sol", "s", nil, tc.owned, fn)
			if tc.ok {
				require.NoError(t, err)
				require.NotNil(t, r)
				require.False(t, released)
			} else {
				require.ErrorIs(t, err, ErrNoAvailableAccounts)
				require.Nil(t, r)
				require.Equal(t, !tc.conflict, released)
			}
			if tc.conflict {
				require.False(t, called)
			}
			require.NoError(t, m.ExpectationsWereMet())
		})
	}
}

func TestSourceTierCanDisableAfterGroupRemoval(t *testing.T) {
	db, m, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	svc := &OpenAIGatewayService{sourceTiers: &sourceTierStore{db: db}}
	cfg := defaultSourceTierConfig()
	cfg.EntryGroups = []int64{999}
	cfg.SelfGroups = []int64{998}
	cfg.ExternalGroups = []int64{997}
	m.ExpectExec("INSERT INTO settings").WithArgs(sourceTierKey, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	require.NoError(t, svc.SaveSourceTierConfig(context.Background(), cfg))
	require.NoError(t, m.ExpectationsWereMet())
}

func TestSourceTierScopeDefaultsAndLegacy(t *testing.T) {
	for _, tc := range []struct{ raw, scope string }{
		{"{}", "all"},
		{`{"enabled":false,"entry_groups":[],"self_groups":[10]}`, "all"},
		{`{"enabled":false,"entry_groups":[30]}`, "selected"},
		{`{"enabled":true,"entry_groups":[30]}`, "selected"},
		{`{"enabled":true,"entry_groups":[]}`, "selected"},
		{`{"enabled":true,"entry_scope":"all","entry_groups":[]}`, "all"},
		{`{"entry_scope":"exclude","excluded_entry_groups":[30]}`, "exclude"},
	} {
		c, err := decodeSourceTierConfig(tc.raw)
		require.NoError(t, err)
		require.Equal(t, tc.scope, c.EntryScope)
	}
}
func TestSourceTierScopeMembership(t *testing.T) {
	c := defaultSourceTierConfig()
	require.True(t, c.matchesEntryGroup(30))
	require.True(t, c.matchesEntryGroup(99999))
	require.False(t, c.matchesEntryGroup(0))
	c.EntryScope = "selected"
	c.EntryGroups = []int64{30}
	require.True(t, c.matchesEntryGroup(30))
	require.False(t, c.matchesEntryGroup(99999))
	c.EntryScope = "exclude"
	c.ExcludedEntryGroups = []int64{30}
	require.False(t, c.matchesEntryGroup(30))
	require.True(t, c.matchesEntryGroup(99999))
	c.EntryScope = "invalid"
	require.False(t, c.matchesEntryGroup(30))
	require.Error(t, validateSourceTierConfig(c))
}
func TestSourceTierAllScopeStillRequiresSourcesAndModel(t *testing.T) {
	c := defaultSourceTierConfig()
	c.Enabled = true
	c.SelfGroups = []int64{10}
	c.ExternalGroups = []int64{20}
	require.NoError(t, validateSourceTierConfig(c))
	c.EntryScope = "selected"
	require.Error(t, validateSourceTierConfig(c))
	c.EntryScope = "exclude"
	require.NoError(t, validateSourceTierConfig(c))
	c.Models = nil
	require.Error(t, validateSourceTierConfig(c))
}
func TestSourceTierScopeChecksActualGroupPlatform(t *testing.T) {
	for _, activeOpenAI := range []bool{true, false} {
		db, m, err := sqlmock.New()
		require.NoError(t, err)
		m.ExpectQuery("SELECT EXISTS.*platform='openai'").WithArgs(int64(30)).WillReturnRows(sqlmock.NewRows([]string{"eligible"}).AddRow(activeOpenAI))
		s := &OpenAIGatewayService{sourceTiers: &sourceTierStore{db: db}}
		ok, err := s.sourceTierEntryIsOpenAI(context.Background(), 30)
		require.NoError(t, err)
		require.Equal(t, activeOpenAI, ok)
		require.NoError(t, m.ExpectationsWereMet())
		db.Close()
	}
}
