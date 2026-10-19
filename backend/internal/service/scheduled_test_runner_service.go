package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/robfig/cron/v3"
)

const scheduledTestDefaultMaxWorkers = 10

// ScheduledTestRunnerService periodically scans due test plans and executes them.
type ScheduledTestRunnerService struct {
	db             *sql.DB
	planRepo       ScheduledTestPlanRepository
	scheduledSvc   *ScheduledTestService
	accountTestSvc *AccountTestService
	rateLimitSvc   *RateLimitService
	cfg            *config.Config

	cron           *cron.Cron
	startOnce      sync.Once
	stopOnce       sync.Once
	running        sync.Map
	reasoningSlots chan struct{}
}

// NewScheduledTestRunnerService creates a new runner.
func NewScheduledTestRunnerService(
	planRepo ScheduledTestPlanRepository,
	scheduledSvc *ScheduledTestService,
	accountTestSvc *AccountTestService,
	rateLimitSvc *RateLimitService,
	cfg *config.Config,
) *ScheduledTestRunnerService {
	return &ScheduledTestRunnerService{
		planRepo:       planRepo,
		scheduledSvc:   scheduledSvc,
		accountTestSvc: accountTestSvc,
		rateLimitSvc:   rateLimitSvc,
		cfg:            cfg,
		reasoningSlots: make(chan struct{}, 2),
	}
}

// Start begins the cron ticker (every minute).
func (s *ScheduledTestRunnerService) Start() {
	if s == nil {
		return
	}
	s.startOnce.Do(func() {
		loc := time.Local
		if s.cfg != nil {
			if parsed, err := time.LoadLocation(s.cfg.Timezone); err == nil && parsed != nil {
				loc = parsed
			}
		}

		c := cron.New(cron.WithParser(scheduledTestCronParser), cron.WithLocation(loc), cron.WithChain(cron.SkipIfStillRunning(cron.DefaultLogger)))
		_, err := c.AddFunc("* * * * *", func() { s.runScheduled() })
		if err != nil {
			logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] not started (invalid schedule): %v", err)
			return
		}
		s.cron = c
		s.cron.Start()
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] started (tick=every minute)")
	})
}

// Stop gracefully shuts down the cron scheduler.
func (s *ScheduledTestRunnerService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		if s.cron != nil {
			ctx := s.cron.Stop()
			select {
			case <-ctx.Done():
			case <-time.After(3 * time.Second):
				logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] cron stop timed out")
			}
		}
	})
}

func (s *ScheduledTestRunnerService) runScheduled() {
	if !scheduledDeploymentActive() {
		return
	}
	// Delay 10s so execution lands at ~:10 of each minute instead of :00.
	time.Sleep(10 * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	now := time.Now()
	plans, err := s.planRepo.ListDue(ctx, now)
	if err != nil {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] ListDue error: %v", err)
		return
	}
	if len(plans) == 0 {
		return
	}

	logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] found %d due plans", len(plans))

	sem := make(chan struct{}, scheduledTestDefaultMaxWorkers)
	var wg sync.WaitGroup

	for _, plan := range plans {
		sem <- struct{}{}
		wg.Add(1)
		go func(p *ScheduledTestPlan) {
			defer wg.Done()
			defer func() { <-sem }()
			s.runOnePlan(ctx, p)
		}(plan)
	}

	wg.Wait()
}

func (s *ScheduledTestRunnerService) runOnePlan(ctx context.Context, plan *ScheduledTestPlan) {
	if _, err := s.executePlan(ctx, plan, true); err != nil {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d execution error: %v", plan.ID, err)
	}
}

// ExecutePlan is shared by scheduled and manual runs. The account lock prevents
// overlapping probes on one account; the cron guard prevents overlapping scans.
func (s *ScheduledTestRunnerService) ExecutePlan(ctx context.Context, plan *ScheduledTestPlan) (*ScheduledTestResult, error) {
	return s.executePlan(ctx, plan, false)
}

func (s *ScheduledTestRunnerService) executePlan(ctx context.Context, plan *ScheduledTestPlan, scheduled bool) (*ScheduledTestResult, error) {
	if _, loaded := s.running.LoadOrStore(plan.AccountID, true); loaded {
		return nil, fmt.Errorf("account test already running")
	}
	defer s.running.Delete(plan.AccountID)
	if s.db != nil {
		release, acquired, err := lockScheduledAccount(ctx, s.db, plan.AccountID)
		if err != nil {
			return nil, err
		}
		if !acquired {
			return nil, fmt.Errorf("account test already running on another instance")
		}
		defer release()
	}
	if scheduled {
		if !scheduledDeploymentActive() {
			return nil, nil
		}
		fresh, err := s.planRepo.GetByID(ctx, plan.ID)
		if err != nil {
			return nil, err
		}
		if !fresh.Enabled || fresh.NextRunAt == nil || fresh.NextRunAt.After(time.Now()) {
			return nil, nil
		}
		plan = fresh
	}
	ctx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()
	var result *ScheduledTestResult
	var err error
	if plan.TestKind == ScheduledTestCandyV1 {
		select {
		case s.reasoningSlots <- struct{}{}:
			defer func() { <-s.reasoningSlots }()
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		result, err = s.accountTestSvc.RunReasoningProbe(ctx, plan.AccountID)
	} else {
		result, err = s.accountTestSvc.RunTestBackground(ctx, plan.AccountID, plan.ModelID)
	}
	if err != nil {
		return nil, err
	}

	// Persist timeout/failure results even if the probe exhausted its context.
	persistCtx, persistCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer persistCancel()
	ctx = persistCtx
	if err := s.scheduledSvc.SaveResult(ctx, plan.ID, plan.MaxResults, result); err != nil {
		return nil, err
	}

	// Auto-recover account if test succeeded and auto_recover is enabled.
	if result.Status == "success" && plan.AutoRecover && plan.TestKind != ScheduledTestCandyV1 {
		s.tryRecoverAccount(ctx, plan.AccountID, plan.ID)
	}

	nextRun, err := computeNextRun(plan.CronExpression, time.Now())
	if err != nil {
		return nil, err
	}

	if err := s.planRepo.UpdateAfterRun(ctx, plan.ID, time.Now(), nextRun); err != nil {
		return nil, err
	}
	return result, nil
}

// tryRecoverAccount attempts to recover an account from recoverable runtime state.
func (s *ScheduledTestRunnerService) tryRecoverAccount(ctx context.Context, accountID int64, planID int64) {
	if s.rateLimitSvc == nil {
		return
	}

	recovery, err := s.rateLimitSvc.RecoverAccountAfterSuccessfulTest(ctx, accountID)
	if err != nil {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d auto-recover failed: %v", planID, err)
		return
	}
	if recovery == nil {
		return
	}

	if recovery.ClearedError {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d auto-recover: account=%d recovered from error status", planID, accountID)
	}
	if recovery.ClearedRateLimit {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d auto-recover: account=%d cleared rate-limit/runtime state", planID, accountID)
	}
}

// An optional owner marker lets a warm/draining blue-green instance skip cron scans.
func scheduledDeploymentActive() bool {
	slot := os.Getenv("SUB2API_DEPLOYMENT_SLOT")
	file := os.Getenv("SUB2API_SCHEDULER_OWNER_FILE")
	if slot == "" || file == "" {
		return true
	}
	data, err := os.ReadFile(file)
	return err == nil && strings.TrimSpace(string(data)) == slot
}

func lockScheduledAccount(ctx context.Context, db *sql.DB, accountID int64) (func(), bool, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, false, err
	}
	key := int64(7319000000000000) + accountID
	var acquired bool
	if err = conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", key).Scan(&acquired); err != nil || !acquired {
		conn.Close()
		return nil, false, err
	}
	return func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := conn.ExecContext(releaseCtx, "SELECT pg_advisory_unlock($1)", key)
		if err != nil { // Discard a session whose lock release is uncertain.
			conn.Raw(func(any) error { return driver.ErrBadConn })
		}
		conn.Close()
	}, true, nil
}
