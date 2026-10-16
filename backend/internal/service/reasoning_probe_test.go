package service

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCandyReasoningEvaluation(t *testing.T) {
	for _, tc := range []struct {
		name, text, status, verdict string
		contains                    *bool
	}{
		{"answer", "最少21个", "success", "not_degraded", boolPtrProbe(true)},
		{"reasoning", "21不够，答案29", "success", "not_degraded", boolPtrProbe(true)},
		{"wrong", "29", "success", "degraded", boolPtrProbe(false)},
		{"boundaries", "121 210 21.5 -21", "success", "degraded", boolPtrProbe(false)},
		{"full width", "２１个", "success", "not_degraded", boolPtrProbe(true)},
		{"no digits", "无法回答", "success", "degraded", boolPtrProbe(false)},
		{"empty", " ", "success", "inconclusive", nil},
		{"partial failure", "21", "failed", "inconclusive", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &ScheduledTestResult{TestKind: ScheduledTestCandyV1, Status: tc.status, ResponseText: tc.text}
			EvaluateReasoningProbe(r)
			require.Equal(t, tc.verdict, r.Quality.Verdict)
			require.Equal(t, tc.contains, r.Quality.Contains21)
			require.NotNil(t, r.Quality.Numbers)
		})
	}
	r := &ScheduledTestResult{Status: "success", ResponseText: "21", TestKind: ScheduledTestConnectivity}
	EvaluateReasoningProbe(r)
	require.Nil(t, r.Quality)
}
func boolPtrProbe(v bool) *bool { return &v }

func TestCandyReasoningPayloadAndValidation(t *testing.T) {
	p := createOpenAITestPayload(CandyReasoningModel, true, CandyReasoningPrompt)
	configureReasoningPayload(p, false)
	require.Equal(t, CandyReasoningPrompt, p["input"].([]map[string]any)[0]["content"].([]map[string]any)[0]["text"])
	require.Equal(t, "none", p["tool_choice"])
	require.Empty(t, p["tools"])
	require.Equal(t, false, p["store"])
	require.Equal(t, map[string]any{"effort": "high"}, p["reasoning"])
	require.NotContains(t, CandyReasoningPrompt, "21")
	require.Equal(t, "hi", createOpenAITestPayload("model", true)["input"].([]map[string]any)[0]["content"].([]map[string]any)[0]["text"])
	plan := &ScheduledTestPlan{TestKind: ScheduledTestCandyV1, AutoRecover: true}
	require.NoError(t, validateScheduledTestKind(plan))
	require.Equal(t, CandyReasoningModel, plan.ModelID)
	require.False(t, plan.AutoRecover)
	plan.ModelID = "gpt-6-sol"
	require.Error(t, validateScheduledTestKind(plan))
	require.Error(t, validateScheduledTestKind(&ScheduledTestPlan{TestKind: "unknown"}))
}
