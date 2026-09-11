//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSerialModelsValidation(t *testing.T) {
	for _, models := range [][]string{nil, {"a", "a"}, {"*"}, {"a b"}, {" a"}, {""}, make([]string, 31)} {
		require.Error(t, ValidateSerialModels(models))
	}
	require.NoError(t, ValidateSerialModels([]string{"gpt-6-astra", "gpt-5.4"}))
}

func TestSerialResultRequiresCompletion(t *testing.T) {
	for _, body := range []string{"", `data: {"type":"content","text":"hello"}`, `data: {"type":"test_complete","success":false}`} {
		require.Equal(t, "failed", parseSerialModelResult("a", body, nil).Status)
	}
	body := "data: {\"type\":\"test_start\",\"model\":\"a\"}\n" + "data:{\"type\":\"test_complete\",\"success\":true}\n"
	result := parseSerialModelResult("a", body, nil)
	require.Equal(t, "success", result.Status)
	require.Equal(t, "a", result.ActualModel)
	require.Equal(t, "failed", parseSerialModelResult("a", body, errors.New("timeout")).Status)
	require.Equal(t, "failed", parseSerialModelResult("a", "data:{\"type\":\"error\",\"error\":\"failed\"}\n"+body, nil).Status)
}

func TestSerialTestLock(t *testing.T) {
	s := &AccountTestService{}
	release, ok := s.AcquireSerialTest(1)
	require.True(t, ok)
	_, ok = s.AcquireSerialTest(1)
	require.False(t, ok)
	otherRelease, ok := s.AcquireSerialTest(2)
	require.True(t, ok)
	otherRelease()
	release()
	release, ok = s.AcquireSerialTest(1)
	require.True(t, ok)
	release()
}

type serialPresetRepo struct{ grokBaseURLSettingRepoStub }

func (r *serialPresetRepo) Set(_ context.Context, key, value string) error {
	r.values[key] = value
	return nil
}

func TestSerialPresetSharedWithinPlatform(t *testing.T) {
	repo := &openAIAccountTestRepo{mockAccountRepoForGemini: mockAccountRepoForGemini{accountsByID: map[int64]*Account{
		1: {ID: 1, Platform: PlatformOpenAI},
		2: {ID: 2, Platform: PlatformOpenAI},
		3: {ID: 3, Platform: PlatformGemini},
	}}}
	settings := &serialPresetRepo{grokBaseURLSettingRepoStub{values: map[string]string{}}}
	s := &AccountTestService{accountRepo: repo, settingService: &SettingService{settingRepo: settings}}
	models := []string{"gpt-6-astra", "gpt-5.4"}
	_, err := s.SerialTestPreset(context.Background(), 1, &models)
	require.NoError(t, err)
	got, err := s.SerialTestPreset(context.Background(), 2, nil)
	require.NoError(t, err)
	require.Equal(t, models, got)
	got, err = s.SerialTestPreset(context.Background(), 3, nil)
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestSerialTestOrderPersistenceAndDirectModel(t *testing.T) {
	account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{
		"access_token": "test-token", "model_mapping": map[string]any{"*": "wrong-target"},
	}}
	repo := &openAIAccountTestRepo{mockAccountRepoForGemini: mockAccountRepoForGemini{accountsByID: map[int64]*Account{1: account}}}
	success := newJSONResponse(200, "data: {\"type\":\"response.completed\"}\n\n")
	upstream := &queuedHTTPUpstream{responses: []*http.Response{newJSONResponse(400, `{"error":{"message":"unsupported model"}}`), success}}
	s := &AccountTestService{accountRepo: repo, httpUpstream: upstream}
	models := []string{"custom-a", "custom-b"}
	var final SerialTestRun
	err := s.RunSerialTest(context.Background(), 1, models, func(kind string, data any) {
		if kind != "snapshot" && kind != "complete" {
			return
		}
		run := data.(*SerialTestRun)
		if run.Results[0].Status == "running" {
			require.Empty(t, upstream.requests)
		}
		if run.Results[1].Status == "running" {
			require.Len(t, upstream.requests, 1)
			require.Equal(t, "failed", run.Results[0].Status)
		}
		if kind == "complete" {
			final = *run
		}
	})
	require.NoError(t, err)
	require.Equal(t, "completed", final.Status)
	require.Equal(t, "failed", final.Results[0].Status)
	require.Equal(t, "success", final.Results[1].Status)
	require.Equal(t, "wrong-target", account.GetMappedModel("custom-a"))
	for i, req := range upstream.requests {
		body, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		var payload map[string]any
		require.NoError(t, json.Unmarshal(body, &payload))
		require.Equal(t, models[i], payload["model"])
	}
	require.Equal(t, "completed", repo.updatedExtra[SerialTestExtraKey].(*SerialTestRun).Status)
}

func TestSerialTestCancellationPersistsSkippedModels(t *testing.T) {
	account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "test"}}
	repo := &openAIAccountTestRepo{mockAccountRepoForGemini: mockAccountRepoForGemini{accountsByID: map[int64]*Account{1: account}}}
	upstream := &queuedHTTPUpstream{responses: []*http.Response{newJSONResponse(200, "data: {\"type\":\"response.completed\"}\n\n")}}
	s := &AccountTestService{accountRepo: repo, httpUpstream: upstream}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := s.RunSerialTest(ctx, 1, []string{"gpt-6-astra", "gpt-5.4"}, func(kind string, data any) {
		if kind == "snapshot" && data.(*SerialTestRun).Results[0].Status == "success" {
			cancel()
		}
	})
	require.NoError(t, err)
	run := repo.updatedExtra[SerialTestExtraKey].(*SerialTestRun)
	require.Equal(t, "cancelled", run.Status)
	require.Equal(t, "skipped", run.Results[1].Status)
	require.Len(t, upstream.requests, 1)
	require.NotNil(t, run.FinishedAt)
	require.False(t, strings.Contains(run.Results[0].Error, "test"))
}
