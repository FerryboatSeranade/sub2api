package service

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const ScheduledTestConnectivity = "connectivity"
const ScheduledTestCandyV1 = "candy_v1"
const CandyReasoningModel = "gpt-6.1-sol"

// Keep the prompt and scoring rule versioned. Never include the expected answer
// in the request: scoring is performed locally after receiving the model output.
const CandyReasoningPrompt = `# 不使用任何外部工具回答以下问题：

在一个黑色的袋子里放有三种口味的糖果，每种糖果有两种不同的形状（圆形和五角星形，不同的形状靠手感可以分辨）。现已知不同口味的糖和不同形状的数量统计如下表。参赛者需要在活动前决定摸出的糖果数目，那么，最少取出多少个糖果才能保证手中同时拥有不同形状的苹果味和桃子味的糖？（同时手中有圆形苹果味匹配五角星桃子味糖果，或者有圆形桃子味匹配五角星苹果味糖果都满足要求）

            苹果味  桃子味  西瓜味
圆形          7      9      8
五角星形      7      6      4`

type ReasoningProbeEvaluation struct {
	Verdict    string   `json:"verdict"`
	Numbers    []string `json:"numbers"`
	Contains21 *bool    `json:"contains_21"`
}

var reasoningNumbers = regexp.MustCompile(`[+-]?[0-9]+(?:\.[0-9]+)?`)

// The requested heuristic scores all visible answer text, not just its final
// conclusion. It is not a general intelligence measurement. Failed calls and
// empty responses must never be classified as degraded.
func EvaluateReasoningProbe(result *ScheduledTestResult) {
	if result == nil || result.TestKind != ScheduledTestCandyV1 {
		return
	}
	q := &ReasoningProbeEvaluation{Verdict: "inconclusive", Numbers: []string{}}
	text := strings.Map(func(r rune) rune {
		if r >= '０' && r <= '９' {
			return r - '０' + '0'
		}
		return r
	}, result.ResponseText)
	if nums := reasoningNumbers.FindAllString(text, -1); nums != nil {
		q.Numbers = nums
	}
	if result.Status == "success" && strings.TrimSpace(text) != "" {
		found := false
		for _, n := range q.Numbers {
			if n == "21" {
				found = true
				break
			}
		}
		q.Contains21 = &found
		q.Verdict = "degraded"
		if found {
			q.Verdict = "not_degraded"
		}
	}
	result.Quality = q
}

func validateScheduledTestKind(plan *ScheduledTestPlan) error {
	if plan.TestKind == "" {
		plan.TestKind = ScheduledTestConnectivity
	}
	switch plan.TestKind {
	case ScheduledTestConnectivity:
	case ScheduledTestCandyV1:
		if plan.ModelID == "" {
			plan.ModelID = CandyReasoningModel
		}
		if plan.ModelID != CandyReasoningModel {
			return fmt.Errorf("candy_v1 requires model %s", CandyReasoningModel)
		}
		// Reasoning results must not alter account scheduling or health status.
		plan.AutoRecover = false
	default:
		return fmt.Errorf("unsupported test_kind: %s", plan.TestKind)
	}
	if plan.MaxResults > 500 {
		return fmt.Errorf("max_results must not exceed 500")
	}
	return nil
}

func configureReasoningPayload(payload map[string]any, chat bool) {
	payload["tools"] = []any{}
	payload["tool_choice"] = "none"
	if chat {
		payload["reasoning_effort"] = "high"
	} else {
		payload["reasoning"] = map[string]any{"effort": "high"}
		payload["instructions"] = "请独立回答用户的问题，不使用任何外部工具。"
	}
}

func (s *AccountTestService) RunReasoningProbe(ctx context.Context, accountID int64) (*ScheduledTestResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account.Platform != PlatformOpenAI || account.IsSyntheticUITest() {
		now := time.Now()
		r := &ScheduledTestResult{Status: "failed", TestKind: ScheduledTestCandyV1, ModelID: CandyReasoningModel, ErrorMessage: "Account does not support an OpenAI reasoning probe", StartedAt: now, FinishedAt: now}
		EvaluateReasoningProbe(r)
		return r, nil
	}
	result, err := s.runTestBackground(ctx, accountID, CandyReasoningModel, CandyReasoningPrompt, true)
	if err != nil {
		return nil, err
	}
	result.TestKind = ScheduledTestCandyV1
	EvaluateReasoningProbe(result)
	return result, nil
}
