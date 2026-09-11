package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
)

const SerialTestExtraKey = "latest_serial_model_test"
const serialTestPresetKey = "account_serial_test_models_"

type SerialModelResult struct {
	Model       string `json:"model"`
	ActualModel string `json:"actual_model,omitempty"`
	Status      string `json:"status"`
	Error       string `json:"error,omitempty"`
	LatencyMS   int64  `json:"latency_ms"`
}

type SerialTestRun struct {
	StartedAt  time.Time           `json:"started_at"`
	FinishedAt *time.Time          `json:"finished_at,omitempty"`
	Status     string              `json:"status"`
	Results    []SerialModelResult `json:"results"`
}

func ValidateSerialModels(models []string) error {
	if len(models) == 0 || len(models) > 30 {
		return errors.New("provide between 1 and 30 models")
	}
	seen := make(map[string]bool, len(models))
	for _, model := range models {
		if model == "" || len(model) > 200 || strings.IndexFunc(model, unicode.IsSpace) >= 0 || strings.Contains(model, "*") || seen[model] {
			return errors.New("models must be unique explicit model IDs, without whitespace or wildcards")
		}
		seen[model] = true
	}
	return nil
}

func (s *AccountTestService) SerialTestPreset(ctx context.Context, accountID int64, models *[]string) ([]string, error) {
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if s.settingService == nil || s.settingService.settingRepo == nil {
		return nil, errors.New("settings service unavailable")
	}
	key := serialTestPresetKey + account.Platform
	if models != nil {
		if err := ValidateSerialModels(*models); err != nil {
			return nil, err
		}
		data, err := json.Marshal(*models)
		if err != nil {
			return nil, err
		}
		return *models, s.settingService.settingRepo.Set(ctx, key, string(data))
	}
	values, err := s.settingService.settingRepo.GetMultiple(ctx, []string{key})
	if err != nil {
		return nil, err
	}
	result := []string{}
	if values[key] != "" {
		err = json.Unmarshal([]byte(values[key]), &result)
	}
	return result, err
}

// The lock covers the entire batch, including persistence. Different accounts
// may be tested concurrently, but a second batch for this account is rejected.
func (s *AccountTestService) AcquireSerialTest(accountID int64) (func(), bool) {
	_, loaded := s.serialTests.LoadOrStore(accountID, true)
	if loaded {
		return nil, false
	}
	return func() { s.serialTests.Delete(accountID) }, true
}

func parseSerialModelResult(model, body string, testErr error) SerialModelResult {
	result := SerialModelResult{Model: model, Status: "failed", Error: "Stream ended without a successful completion event"}
	completed, failed := false, false
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var event TestEvent
		if json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &event) != nil {
			continue
		}
		if event.Type == "test_start" && event.Model != "" {
			result.ActualModel = event.Model
		}
		if event.Type == "error" || (event.Type == "test_complete" && !event.Success) {
			failed = true
			result.Error = event.Error
			if result.Error == "" {
				result.Error = "Test failed"
			}
		}
		if event.Type == "test_complete" && event.Success {
			completed = true
		}
	}
	if testErr != nil {
		failed = true
		result.Error = testErr.Error()
	}
	if completed && !failed {
		result.Status = "success"
		result.Error = ""
	}
	if len(result.Error) > 1500 {
		result.Error = result.Error[:1500]
	}
	return result
}

func (s *AccountTestService) probeSerialModel(ctx context.Context, accountID int64, model string) (result SerialModelResult) {
	started := time.Now()
	defer func() {
		if recovered := recover(); recovered != nil {
			result = SerialModelResult{Model: model, Status: "failed", Error: "Internal probe error"}
		}
		result.LatencyMS = time.Since(started).Milliseconds()
	}()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/", nil).WithContext(ctx)
	err := s.TestAccountConnection(c, accountID, model, "", AccountTestModeDefault, AccountTestOptions{DirectModel: true})
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	result = parseSerialModelResult(model, recorder.Body.String(), err)
	return result
}

func (s *AccountTestService) RunSerialTest(ctx context.Context, accountID int64, models []string, emit func(string, any)) error {
	if err := ValidateSerialModels(models); err != nil {
		return err
	}
	run := &SerialTestRun{StartedAt: time.Now(), Status: "running", Results: make([]SerialModelResult, len(models))}
	for i, model := range models {
		run.Results[i] = SerialModelResult{Model: model, Status: "pending"}
	}
	save := func() error {
		// Disconnecting the browser cancels probes, but must not lose the latest results.
		saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		return s.accountRepo.UpdateExtra(saveCtx, accountID, map[string]any{SerialTestExtraKey: run})
	}
	if err := save(); err != nil {
		return err
	}
	emit("snapshot", run)
	for i, model := range models {
		if ctx.Err() != nil {
			break
		}
		run.Results[i].Status = "running"
		emit("snapshot", run)
		probeCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
		done := make(chan SerialModelResult, 1)
		go func() { done <- s.probeSerialModel(probeCtx, accountID, model) }()
		ticker := time.NewTicker(10 * time.Second)
		waiting := true
		for waiting {
			select {
			case result := <-done:
				if ctx.Err() != nil {
					result.Status = "skipped"
				}
				run.Results[i] = result
				waiting = false
			case <-ticker.C:
				emit("heartbeat", nil)
			}
		}
		ticker.Stop()
		cancel()
		if err := save(); err != nil {
			return fmt.Errorf("persist test result: %w", err)
		}
		emit("snapshot", run)
	}
	run.Status = "completed"
	if ctx.Err() != nil {
		run.Status = "cancelled"
	}
	for i := range run.Results {
		if run.Results[i].Status == "pending" {
			run.Results[i].Status = "skipped"
		}
	}
	now := time.Now()
	run.FinishedAt = &now
	if err := save(); err != nil {
		return err
	}
	emit("complete", run)
	return nil
}
