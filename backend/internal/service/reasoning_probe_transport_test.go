//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCandyReasoningUsesExactModelAndPreservesHealth(t *testing.T) {
	for _, status := range []int{200, 401, 429} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 1, Credentials: map[string]any{"access_token": "fake-test-token", "model_mapping": map[string]any{CandyReasoningModel: "gpt-6-sol"}}}
			repo := &openAIAccountTestRepo{mockAccountRepoForGemini: mockAccountRepoForGemini{accountsByID: map[int64]*Account{1: account}}}
			body := `{"error":{"message":"unavailable"}}`
			if status == 200 {
				body = "data: {\"type\":\"response.output_text.delta\",\"delta\":\"21个\"}\n\ndata: {\"type\":\"response.completed\"}\n\n"
			}
			upstream := &queuedHTTPUpstream{responses: []*http.Response{newJSONResponse(status, body)}}
			svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream}
			result, err := svc.RunReasoningProbe(context.Background(), 1)
			require.NoError(t, err)
			require.Len(t, upstream.requests, 1)
			raw, err := io.ReadAll(upstream.requests[0].Body)
			require.NoError(t, err)
			var payload map[string]any
			require.NoError(t, json.Unmarshal(raw, &payload))
			require.Equal(t, CandyReasoningModel, payload["model"])
			require.Equal(t, "none", payload["tool_choice"])
			require.Empty(t, payload["tools"])
			require.Contains(t, string(raw), "黑色的袋子")
			require.NotContains(t, string(raw), "21")
			require.Equal(t, "gpt-6-sol", account.GetMappedModel(CandyReasoningModel), "probe must not mutate shared model mappings")
			require.Zero(t, repo.setErrorID)
			require.Zero(t, repo.rateLimitedID)
			if status == 200 {
				require.Equal(t, "not_degraded", result.Quality.Verdict)
			} else {
				require.Equal(t, "inconclusive", result.Quality.Verdict)
				require.Nil(t, result.Quality.Contains21)
			}
		})
	}
}

func TestPelicanUsesRequestedModelWithoutHealthMutation(t *testing.T) {
	account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "fake-test-token"}}
	repo := &openAIAccountTestRepo{mockAccountRepoForGemini: mockAccountRepoForGemini{accountsByID: map[int64]*Account{1: account}}}
	body := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"<svg viewBox='0 0 100 100'><circle cx='20' cy='20' r='10'/></svg>\"}\n\ndata: {\"type\":\"response.completed\"}\n\n"
	upstream := &queuedHTTPUpstream{responses: []*http.Response{newJSONResponse(200, body)}}
	svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream}
	result, err := svc.RunPelicanProbe(context.Background(), 1, CandyReasoningModel)
	require.NoError(t, err)
	require.Equal(t, ScheduledTestPelicanV1, result.TestKind)
	require.Nil(t, result.Quality)
	require.Equal(t, "reference_only", result.Pelican.Verdict)
	raw, err := io.ReadAll(upstream.requests[0].Body)
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(raw, &payload))
	require.Equal(t, CandyReasoningModel, payload["model"])
	require.Equal(t, "none", payload["tool_choice"])
	require.Contains(t, string(raw), "pelican riding a bicycle")
	require.Zero(t, repo.setErrorID)
	require.Zero(t, repo.rateLimitedID)
}
