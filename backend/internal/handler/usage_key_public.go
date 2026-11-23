package handler

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

// Public key queries never accept a caller-selected owner or key ID.
func keyUsageFilters(c *gin.Context) (usagestats.UsageLogFilters, bool) {
	c.Header("Cache-Control", "no-store")
	key, ok := middleware.GetAPIKeyFromContext(c)
	if !ok || key == nil || key.ID <= 0 || key.UserID <= 0 {
		response.Unauthorized(c, "Full API key authentication required")
		return usagestats.UsageLogFilters{}, false
	}
	for _, name := range []string{"api_key_id", "user_id", "account_id", "group_id", "model_source"} {
		if _, exists := c.GetQuery(name); exists {
			response.BadRequest(c, name+" is not supported; only this key's records are available")
			return usagestats.UsageLogFilters{}, false
		}
	}
	end := time.Now().UTC()
	if raw := c.Query("end_time"); raw != "" {
		var err error
		end, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			response.BadRequest(c, "end_time must be RFC3339")
			return usagestats.UsageLogFilters{}, false
		}
	}
	start := end.Add(-7 * 24 * time.Hour)
	if raw := c.Query("start_time"); raw != "" {
		var err error
		start, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			response.BadRequest(c, "start_time must be RFC3339")
			return usagestats.UsageLogFilters{}, false
		}
	}
	if !start.Before(end) || end.Sub(start) > 31*24*time.Hour {
		response.BadRequest(c, "Time range must be positive and at most 31 days")
		return usagestats.UsageLogFilters{}, false
	}
	model := strings.TrimSpace(c.Query("model"))
	if len(model) > 200 {
		response.BadRequest(c, "model is too long")
		return usagestats.UsageLogFilters{}, false
	}
	return usagestats.UsageLogFilters{UserID: key.UserID, APIKeyID: key.ID, Model: model, ModelFilterSource: usagestats.ModelSourceRequested, StartTime: &start, EndTime: &end, ExactTotal: true}, true
}

type publicUsageName struct {
	Name string `json:"name"`
}

type publicKeyUsageRecord struct {
	APIKey                    publicUsageName  `json:"api_key"`
	Group                     *publicUsageName `json:"group"`
	ReasoningEffort           *string          `json:"reasoning_effort"`
	InboundEndpoint           *string          `json:"inbound_endpoint"`
	RequestType               string           `json:"request_type"`
	BillingMode               *string          `json:"billing_mode"`
	BillingType               int8             `json:"billing_type"`
	FirstTokenMs              *int             `json:"first_token_ms"`
	InputCost                 float64          `json:"input_cost"`
	OutputCost                float64          `json:"output_cost"`
	CacheCreationCost         float64          `json:"cache_creation_cost"`
	CacheReadCost             float64          `json:"cache_read_cost"`
	RateMultiplier            float64          `json:"rate_multiplier"`
	ImageCount                int              `json:"image_count"`
	ImageInputTokens          int              `json:"image_input_tokens"`
	ImageOutputTokens         int              `json:"image_output_tokens"`
	LongContextBillingApplied bool             `json:"long_context_billing_applied"`
	ID                        int64            `json:"id"`
	RequestID                 string           `json:"request_id"`
	Model                     string           `json:"model"`
	InputTokens               int              `json:"input_tokens"`
	OutputTokens              int              `json:"output_tokens"`
	CacheCreationTokens       int              `json:"cache_creation_tokens"`
	CacheReadTokens           int              `json:"cache_read_tokens"`
	TotalCost                 float64          `json:"total_cost"`
	ActualCost                float64          `json:"actual_cost"`
	DurationMs                *int             `json:"duration_ms"`
	Stream                    bool             `json:"stream"`
	CreatedAt                 time.Time        `json:"created_at"`
}

// KeyRecords is an authenticated, key-scoped public read API, not a panel session.
func (h *UsageHandler) KeyRecords(c *gin.Context) {
	filters, ok := keyUsageFilters(c)
	if !ok {
		return
	}
	page, size := 1, 20
	for name, target := range map[string]*int{"page": &page, "page_size": &size} {
		if raw := c.Query(name); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 {
				response.BadRequest(c, "Invalid "+name)
				return
			}
			*target = n
		}
	}
	if size > 100 || page > 10000 {
		response.BadRequest(c, "page_size must be <= 100 and page <= 10000")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	logs, result, err := h.usageService.ListWithFilters(ctx, pagination.PaginationParams{Page: page, PageSize: size, SortBy: "created_at", SortOrder: "desc"}, filters)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	items := make([]publicKeyUsageRecord, 0, len(logs))
	key, _ := middleware.GetAPIKeyFromContext(c)
	for _, log := range logs {
		var group *publicUsageName
		if log.Group != nil {
			group = &publicUsageName{log.Group.Name}
		}
		model := strings.TrimSpace(log.RequestedModel)
		if model == "" {
			model = log.Model
		}
		items = append(items, publicKeyUsageRecord{APIKey: publicUsageName{key.Name}, Group: group,
			ReasoningEffort:           log.ReasoningEffort,
			InboundEndpoint:           log.InboundEndpoint,
			RequestType:               log.EffectiveRequestType().String(),
			BillingMode:               log.BillingMode,
			BillingType:               log.BillingType,
			FirstTokenMs:              log.FirstTokenMs,
			InputCost:                 log.InputCost,
			OutputCost:                log.OutputCost,
			CacheCreationCost:         log.CacheCreationCost,
			CacheReadCost:             log.CacheReadCost,
			RateMultiplier:            log.RateMultiplier,
			ImageCount:                log.ImageCount,
			ImageInputTokens:          log.ImageInputTokens,
			ImageOutputTokens:         log.ImageOutputTokens,
			LongContextBillingApplied: log.LongContextBillingApplied,
			ID:                        log.ID, RequestID: log.RequestID, Model: model, InputTokens: log.InputTokens, OutputTokens: log.OutputTokens, CacheCreationTokens: log.CacheCreationTokens, CacheReadTokens: log.CacheReadTokens, TotalCost: log.TotalCost, ActualCost: log.ActualCost, DurationMs: log.DurationMs, Stream: log.Stream, CreatedAt: log.CreatedAt})
	}
	response.Paginated(c, items, result.Total, page, size)
}

// KeyModels lists models actually used by this key in the selected interval.
func (h *UsageHandler) KeyModels(c *gin.Context) {
	filters, ok := keyUsageFilters(c)
	if !ok {
		return
	}
	filters.Model = ""
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	stats, err := h.usageService.GetModelStatsWithFiltersBySource(ctx, *filters.StartTime, *filters.EndTime, filters, usagestats.ModelSourceRequested)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	type modelOption struct {
		Model    string `json:"model"`
		Requests int64  `json:"requests"`
	}
	items := make([]modelOption, 0, len(stats))
	for _, s := range stats {
		items = append(items, modelOption{s.Model, s.Requests})
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"items": items, "start_time": filters.StartTime, "end_time": filters.EndTime}})
}

func (h *UsageHandler) KeyOverview(c *gin.Context) {
	f, ok := keyUsageFilters(c)
	if !ok {
		return
	}
	granularity := c.DefaultQuery("granularity", "day")
	if granularity != "day" && granularity != "hour" {
		response.BadRequest(c, "Invalid granularity")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	start, end := *f.StartTime, *f.EndTime
	stats, err := h.usageService.GetStatsWithFilters(ctx, f)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	stats.TotalAccountCost = nil
	stats.UpstreamEndpoints = nil
	stats.EndpointPaths = nil
	for i := range stats.Endpoints {
		stats.Endpoints[i].AccountCost = 0
	}
	models, err := h.usageService.GetModelStatsWithFiltersBySource(ctx, start, end, f, usagestats.ModelSourceRequested)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	for i := range models {
		models[i].AccountCost = 0
	}
	groups, err := h.usageService.GetGroupStatsWithFilters(ctx, start, end, f)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	for i := range groups {
		groups[i].AccountCost = 0
	}
	trend, err := h.usageService.GetUsageTrendWithFilters(ctx, start, end, granularity, f)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"stats": stats, "models": models, "groups": groups, "trend": trend})
}
