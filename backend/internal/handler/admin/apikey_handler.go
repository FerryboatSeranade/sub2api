package admin

import (
	"context"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// AdminAPIKeyHandler handles admin API key management
type AdminAPIKeyHandler struct {
	adminService service.AdminService
}

// NewAdminAPIKeyHandler creates a new admin API key handler
func NewAdminAPIKeyHandler(adminService service.AdminService) *AdminAPIKeyHandler {
	return &AdminAPIKeyHandler{
		adminService: adminService,
	}
}

// AdminUpdateAPIKeyRequest represents the request to update an API key.
type AdminUpdateAPIKeyRequest struct {
	Name            *string   `json:"name"`
	GroupID         *int64    `json:"group_id"` // nil=不修改, 0=解绑, >0=绑定到目标分组
	Status          *string   `json:"status"`
	IPWhitelist     *[]string `json:"ip_whitelist"`
	IPBlacklist     *[]string `json:"ip_blacklist"`
	Quota           *float64  `json:"quota"`
	ExtraQuota      *float64  `json:"extra_quota"`
	ExtraQuotaCamel *float64  `json:"extraQuota"`
	ExpiresAt       *string   `json:"expires_at"`  // RFC3339 timestamp, empty string clears expiration
	ResetQuota      *bool     `json:"reset_quota"` // true=重置 quota_used
	// ResetExtraQuota resets extra_quota_used to 0.
	ResetExtraQuota      *bool `json:"reset_extra_quota"`
	ResetExtraQuotaCamel *bool `json:"resetExtraQuota"`

	RateLimit5h         *float64 `json:"rate_limit_5h"`
	RateLimit1d         *float64 `json:"rate_limit_1d"`
	RateLimit7d         *float64 `json:"rate_limit_7d"`
	ResetRateLimitUsage *bool    `json:"reset_rate_limit_usage"` // true=重置 5h/1d/7d 限速用量
}

// AdminCreateAPIKeyRequest represents the request to create an API key for a user.
type AdminCreateAPIKeyRequest struct {
	Name            string   `json:"name" binding:"required"`
	GroupID         *int64   `json:"group_id"`
	CustomKey       *string  `json:"custom_key"`
	IPWhitelist     []string `json:"ip_whitelist"`
	IPBlacklist     []string `json:"ip_blacklist"`
	Quota           *float64 `json:"quota"`
	ExtraQuota      *float64 `json:"extra_quota"`
	ExtraQuotaCamel *float64 `json:"extraQuota"`
	ExpiresAt       *string  `json:"expires_at"`      // RFC3339 timestamp
	ExpiresInDays   *int     `json:"expires_in_days"` // optional relative expiry
	RateLimit5h     *float64 `json:"rate_limit_5h"`
	RateLimit1d     *float64 `json:"rate_limit_1d"`
	RateLimit7d     *float64 `json:"rate_limit_7d"`
}

// CreateForUser handles creating an API key for a user.
// POST /api/v1/admin/users/:id/api-keys
func (h *AdminAPIKeyHandler) CreateForUser(c *gin.Context) {
	userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid user ID")
		return
	}

	var req AdminCreateAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	svcReq := service.AdminCreateAPIKeyInput{
		Name:          req.Name,
		GroupID:       req.GroupID,
		CustomKey:     req.CustomKey,
		IPWhitelist:   req.IPWhitelist,
		IPBlacklist:   req.IPBlacklist,
		ExpiresInDays: req.ExpiresInDays,
	}
	if req.Quota != nil {
		svcReq.Quota = *req.Quota
	}
	if req.ExtraQuota != nil {
		svcReq.ExtraQuota = *req.ExtraQuota
	} else if req.ExtraQuotaCamel != nil {
		svcReq.ExtraQuota = *req.ExtraQuotaCamel
	}
	if req.RateLimit5h != nil {
		svcReq.RateLimit5h = *req.RateLimit5h
	}
	if req.RateLimit1d != nil {
		svcReq.RateLimit1d = *req.RateLimit1d
	}
	if req.RateLimit7d != nil {
		svcReq.RateLimit7d = *req.RateLimit7d
	}
	if req.ExpiresAt != nil {
		if req.ExpiresInDays != nil {
			response.BadRequest(c, "expires_at and expires_in_days cannot both be set")
			return
		}
		if *req.ExpiresAt == "" {
			response.BadRequest(c, "expires_at cannot be empty")
			return
		}
		expiresAt, err := time.Parse(time.RFC3339, *req.ExpiresAt)
		if err != nil {
			response.BadRequest(c, "Invalid expires_at format: "+err.Error())
			return
		}
		svcReq.ExpiresAt = &expiresAt
	}

	idempotencyPayload := struct {
		UserID int64                    `json:"user_id"`
		Body   AdminCreateAPIKeyRequest `json:"body"`
	}{
		UserID: userID,
		Body:   req,
	}
	executeAdminIdempotentJSON(c, "admin.users.api_keys.create", idempotencyPayload, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		key, err := h.adminService.AdminCreateAPIKey(ctx, userID, &svcReq)
		if err != nil {
			return nil, err
		}
		return dto.APIKeyFromService(key), nil
	})
}

// UpdateGroup handles updating an API key's admin-managed fields.
// PUT /api/v1/admin/api-keys/:id
func (h *AdminAPIKeyHandler) UpdateGroup(c *gin.Context) {
	keyID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid API key ID")
		return
	}

	var req AdminUpdateAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	svcReq := service.AdminUpdateAPIKeyInput{
		Name:                req.Name,
		GroupID:             req.GroupID,
		Status:              req.Status,
		IPWhitelist:         req.IPWhitelist,
		IPBlacklist:         req.IPBlacklist,
		Quota:               req.Quota,
		ResetQuota:          req.ResetQuota,
		RateLimit5h:         req.RateLimit5h,
		RateLimit1d:         req.RateLimit1d,
		RateLimit7d:         req.RateLimit7d,
		ResetRateLimitUsage: req.ResetRateLimitUsage,
	}
	if req.ExtraQuota != nil {
		svcReq.ExtraQuota = req.ExtraQuota
	} else {
		svcReq.ExtraQuota = req.ExtraQuotaCamel
	}
	if req.ResetExtraQuota != nil {
		svcReq.ResetExtraQuota = req.ResetExtraQuota
	} else {
		svcReq.ResetExtraQuota = req.ResetExtraQuotaCamel
	}
	if req.ExpiresAt != nil {
		if *req.ExpiresAt == "" {
			svcReq.ClearExpiration = true
		} else {
			t, err := time.Parse(time.RFC3339, *req.ExpiresAt)
			if err != nil {
				response.BadRequest(c, "Invalid expires_at format: "+err.Error())
				return
			}
			svcReq.ExpiresAt = &t
		}
	}

	result, err := h.adminService.AdminUpdateAPIKey(c.Request.Context(), keyID, &svcReq)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	resp := struct {
		APIKey                 *dto.APIKey `json:"api_key"`
		AutoGrantedGroupAccess bool        `json:"auto_granted_group_access"`
		GrantedGroupID         *int64      `json:"granted_group_id,omitempty"`
		GrantedGroupName       string      `json:"granted_group_name,omitempty"`
	}{
		APIKey:                 dto.APIKeyFromService(result.APIKey),
		AutoGrantedGroupAccess: result.AutoGrantedGroupAccess,
		GrantedGroupID:         result.GrantedGroupID,
		GrantedGroupName:       result.GrantedGroupName,
	}
	response.Success(c, resp)
}
