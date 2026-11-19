package handler

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type publicKeyUsageRepo struct {
	userUsageRepoCapture
	modelsFilters usagestats.UsageLogFilters
	source        string
}

func (r *publicKeyUsageRepo) GetModelStatsWithUsageFiltersBySource(_ context.Context, _, _ time.Time, f usagestats.UsageLogFilters, source string) ([]usagestats.ModelStat, error) {
	r.modelsFilters = f
	r.source = source
	return []usagestats.ModelStat{{Model: "gpt-6.1-sol", Requests: 2}}, nil
}
func publicKeyUsageRouter(repo *publicKeyUsageRepo, authenticated bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if authenticated {
			c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 23, UserID: 7})
		}
		c.Next()
	})
	h := NewUsageHandler(service.NewUsageService(repo, nil, nil, nil), nil, nil, nil)
	r.GET("/records", h.KeyRecords)
	r.GET("/models", h.KeyModels)
	return r
}
func TestPublicKeyUsageRecordsIsolationAndRedaction(t *testing.T) {
	repo := &publicKeyUsageRepo{}
	repo.listRows = []service.UsageLog{{ID: 1, AccountID: 999, UserID: 7, APIKeyID: 23, Model: "upstream-secret-model", RequestedModel: "gpt-6.1-sol", RequestID: "req-test", ActualCost: 0.2}}
	r := publicKeyUsageRouter(repo, true)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/records?model=gpt-6.1-sol&page=2&page_size=5", nil))
	require.Equal(t, 200, w.Code)
	require.Equal(t, int64(23), repo.listFilters.APIKeyID)
	require.Equal(t, int64(7), repo.listFilters.UserID)
	require.Equal(t, "gpt-6.1-sol", repo.listFilters.Model)
	require.Equal(t, usagestats.ModelSourceRequested, repo.listFilters.ModelFilterSource)
	require.Equal(t, 2, repo.listParams.Page)
	require.Equal(t, 5, repo.listParams.PageSize)
	require.True(t, repo.listFilters.ExactTotal)
	require.Contains(t, w.Body.String(), "gpt-6.1-sol")
	for _, secret := range []string{"account_id", "api_key_id", "user_id", "upstream-secret-model", "credentials"} {
		require.NotContains(t, w.Body.String(), secret)
	}
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
}
func TestPublicKeyUsageModelsScoped(t *testing.T) {
	repo := &publicKeyUsageRepo{}
	r := publicKeyUsageRouter(repo, true)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/models?model=ignored&start_time=2026-11-01T00:00:00Z&end_time=2026-11-10T00:00:00Z", nil))
	require.Equal(t, 200, w.Code)
	require.Equal(t, int64(23), repo.modelsFilters.APIKeyID)
	require.Equal(t, int64(7), repo.modelsFilters.UserID)
	require.Empty(t, repo.modelsFilters.Model)
	require.Equal(t, "requested", repo.source)
	require.Contains(t, w.Body.String(), "gpt-6.1-sol")
}
func TestPublicKeyUsageRejectsUnsafeFilters(t *testing.T) {
	for _, q := range []string{"user_id=8", "api_key_id=24", "account_id=1", "group_id=1", "model_source=upstream", "page=0", "page_size=101", "page=10001", "start_time=bad", "start_time=2026-01-01T00:00:00Z&end_time=2026-11-01T00:00:00Z", "start_time=2026-11-02T00:00:00Z&end_time=2026-11-01T00:00:00Z"} {
		t.Run(q, func(t *testing.T) {
			repo := &publicKeyUsageRepo{}
			w := httptest.NewRecorder()
			publicKeyUsageRouter(repo, true).ServeHTTP(w, httptest.NewRequest("GET", "/records?"+q, nil))
			require.Equal(t, 400, w.Code)
			require.Zero(t, repo.listFilters.APIKeyID)
		})
	}
	for _, path := range []string{"/records", "/models"} {
		w := httptest.NewRecorder()
		publicKeyUsageRouter(&publicKeyUsageRepo{}, false).ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		require.Equal(t, 401, w.Code)
	}
}
