package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

// AdminCreateAPIKey creates an API key for a target user using administrator privileges.
func (s *adminServiceImpl) AdminCreateAPIKey(ctx context.Context, userID int64, input *AdminCreateAPIKeyInput) (*APIKey, error) {
	if input == nil {
		return nil, infraerrors.BadRequest("INVALID_INPUT", "request body is required")
	}
	if userID <= 0 {
		return nil, infraerrors.BadRequest("INVALID_USER_ID", "user_id must be greater than 0")
	}

	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, infraerrors.BadRequest("API_KEY_NAME_REQUIRED", "name is required")
	}
	if len([]rune(name)) > 100 {
		return nil, infraerrors.BadRequest("API_KEY_NAME_TOO_LONG", "name must be at most 100 characters")
	}
	if input.Quota < 0 || input.ExtraQuota < 0 || input.RateLimit5h < 0 || input.RateLimit1d < 0 || input.RateLimit7d < 0 {
		return nil, infraerrors.BadRequest("API_KEY_LIMIT_INVALID", "quota, extra quota, and rate limits must be non-negative")
	}
	if input.ExpiresAt != nil && input.ExpiresInDays != nil {
		return nil, infraerrors.BadRequest("API_KEY_EXPIRY_CONFLICT", "expires_at and expires_in_days cannot both be set")
	}
	if input.ExpiresAt != nil && !input.ExpiresAt.After(time.Now()) {
		return nil, infraerrors.BadRequest("API_KEY_EXPIRES_AT_INVALID", "expires_at must be in the future")
	}
	if input.ExpiresInDays != nil && *input.ExpiresInDays <= 0 {
		return nil, infraerrors.BadRequest("API_KEY_EXPIRES_IN_DAYS_INVALID", "expires_in_days must be greater than zero")
	}
	if invalid := ip.ValidateIPPatterns(input.IPWhitelist); len(invalid) > 0 {
		return nil, fmt.Errorf("%w: %v", ErrInvalidIPPattern, invalid)
	}
	if invalid := ip.ValidateIPPatterns(input.IPBlacklist); len(invalid) > 0 {
		return nil, fmt.Errorf("%w: %v", ErrInvalidIPPattern, invalid)
	}

	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}

	var group *Group
	var groupID *int64
	if input.GroupID != nil {
		if *input.GroupID <= 0 {
			return nil, infraerrors.BadRequest("INVALID_GROUP_ID", "group_id must be greater than 0")
		}
		group, err = s.groupRepo.GetByID(ctx, *input.GroupID)
		if err != nil {
			return nil, err
		}
		if group.Status != StatusActive {
			return nil, infraerrors.BadRequest("GROUP_NOT_ACTIVE", "target group is not active")
		}
		if group.IsSubscriptionType() {
			if s.userSubRepo == nil {
				return nil, infraerrors.InternalServer("SUBSCRIPTION_REPOSITORY_UNAVAILABLE", "subscription repository is not configured")
			}
			if _, err := s.userSubRepo.GetActiveByUserIDAndGroupID(ctx, user.ID, *input.GroupID); err != nil {
				if errors.Is(err, ErrSubscriptionNotFound) {
					return nil, infraerrors.BadRequest("SUBSCRIPTION_REQUIRED", "user does not have an active subscription for this group")
				}
				return nil, err
			}
		}
		gid := *input.GroupID
		groupID = &gid
	}

	keyValue := ""
	if input.CustomKey != nil {
		keyValue = strings.TrimSpace(*input.CustomKey)
	}
	if keyValue != "" {
		if err := validateCustomAPIKey(keyValue); err != nil {
			return nil, err
		}
		exists, err := s.apiKeyRepo.ExistsByKey(ctx, keyValue)
		if err != nil {
			return nil, fmt.Errorf("check key exists: %w", err)
		}
		if exists {
			return nil, ErrAPIKeyExists
		}
	} else {
		keyValue, err = generateAPIKeyWithPrefix("")
		if err != nil {
			return nil, fmt.Errorf("generate key: %w", err)
		}
	}

	expiresAt := input.ExpiresAt
	if expiresAt == nil && input.ExpiresInDays != nil {
		t := time.Now().AddDate(0, 0, *input.ExpiresInDays)
		expiresAt = &t
	}

	apiKey := &APIKey{
		UserID:      user.ID,
		Key:         keyValue,
		Name:        name,
		GroupID:     groupID,
		Status:      StatusActive,
		IPWhitelist: input.IPWhitelist,
		IPBlacklist: input.IPBlacklist,
		Group:       group,
		User:        user,
		Quota:       input.Quota,
		QuotaUsed:   0,
		ExtraQuota:  input.ExtraQuota,
		ExpiresAt:   expiresAt,
		RateLimit5h: input.RateLimit5h,
		RateLimit1d: input.RateLimit1d,
		RateLimit7d: input.RateLimit7d,
	}

	if group != nil && group.IsExclusive && !group.IsSubscriptionType() {
		opCtx := ctx
		var tx *dbent.Tx
		if s.entClient == nil {
			logger.LegacyPrintf("service.admin", "Warning: entClient is nil, skipping transaction protection for exclusive group API key creation")
		} else {
			var txErr error
			tx, txErr = s.entClient.Tx(ctx)
			if txErr != nil {
				return nil, fmt.Errorf("begin transaction: %w", txErr)
			}
			defer func() { _ = tx.Rollback() }()
			opCtx = dbent.NewTxContext(ctx, tx)
		}

		if err := s.userRepo.AddGroupToAllowedGroups(opCtx, user.ID, group.ID); err != nil {
			return nil, fmt.Errorf("add group to user allowed groups: %w", err)
		}
		if err := s.apiKeyRepo.Create(opCtx, apiKey); err != nil {
			return nil, fmt.Errorf("create api key: %w", err)
		}
		if tx != nil {
			if err := tx.Commit(); err != nil {
				return nil, fmt.Errorf("commit transaction: %w", err)
			}
		}
	} else {
		if err := s.apiKeyRepo.Create(ctx, apiKey); err != nil {
			return nil, fmt.Errorf("create api key: %w", err)
		}
	}

	if s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByKey(ctx, apiKey.Key)
	}
	return apiKey, nil
}

// AdminUpdateAPIKey updates admin-managed fields for an API key.
func (s *adminServiceImpl) AdminUpdateAPIKey(ctx context.Context, keyID int64, input *AdminUpdateAPIKeyInput) (*AdminUpdateAPIKeyGroupIDResult, error) {
	if input == nil {
		return nil, infraerrors.BadRequest("INVALID_INPUT", "request body is required")
	}
	if keyID <= 0 {
		return nil, infraerrors.BadRequest("INVALID_API_KEY_ID", "api key id must be greater than 0")
	}

	if input.Quota != nil && *input.Quota < 0 {
		return nil, infraerrors.BadRequest("API_KEY_LIMIT_INVALID", "quota must be non-negative")
	}
	if input.ExtraQuota != nil && *input.ExtraQuota < 0 {
		return nil, infraerrors.BadRequest("API_KEY_LIMIT_INVALID", "extra_quota must be non-negative")
	}
	if input.RateLimit5h != nil && *input.RateLimit5h < 0 {
		return nil, infraerrors.BadRequest("API_KEY_LIMIT_INVALID", "rate_limit_5h must be non-negative")
	}
	if input.RateLimit1d != nil && *input.RateLimit1d < 0 {
		return nil, infraerrors.BadRequest("API_KEY_LIMIT_INVALID", "rate_limit_1d must be non-negative")
	}
	if input.RateLimit7d != nil && *input.RateLimit7d < 0 {
		return nil, infraerrors.BadRequest("API_KEY_LIMIT_INVALID", "rate_limit_7d must be non-negative")
	}
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" {
			return nil, infraerrors.BadRequest("API_KEY_NAME_REQUIRED", "name is required")
		}
		if len([]rune(name)) > 100 {
			return nil, infraerrors.BadRequest("API_KEY_NAME_TOO_LONG", "name must be at most 100 characters")
		}
		*input.Name = name
	}
	if input.Status != nil {
		status := strings.TrimSpace(*input.Status)
		switch status {
		case StatusAPIKeyActive, StatusAPIKeyDisabled, StatusAPIKeyQuotaExhausted, StatusAPIKeyExpired:
			*input.Status = status
		default:
			return nil, infraerrors.BadRequest("API_KEY_STATUS_INVALID", "invalid api key status")
		}
	}
	if input.IPWhitelist != nil {
		if invalid := ip.ValidateIPPatterns(*input.IPWhitelist); len(invalid) > 0 {
			return nil, fmt.Errorf("%w: %v", ErrInvalidIPPattern, invalid)
		}
	}
	if input.IPBlacklist != nil {
		if invalid := ip.ValidateIPPatterns(*input.IPBlacklist); len(invalid) > 0 {
			return nil, fmt.Errorf("%w: %v", ErrInvalidIPPattern, invalid)
		}
	}

	result, err := s.AdminUpdateAPIKeyGroupID(ctx, keyID, input.GroupID)
	if err != nil {
		return nil, err
	}
	apiKey := result.APIKey
	if apiKey == nil {
		return nil, ErrAPIKeyNotFound
	}

	needsUpdate := false
	if input.Name != nil {
		apiKey.Name = *input.Name
		needsUpdate = true
	}
	if input.Status != nil {
		apiKey.Status = *input.Status
		needsUpdate = true
	}
	if input.IPWhitelist != nil {
		apiKey.IPWhitelist = *input.IPWhitelist
		needsUpdate = true
	}
	if input.IPBlacklist != nil {
		apiKey.IPBlacklist = *input.IPBlacklist
		needsUpdate = true
	}
	if input.Quota != nil {
		apiKey.Quota = *input.Quota
		if apiKey.Status == StatusAPIKeyQuotaExhausted && *input.Quota > apiKey.QuotaUsed {
			apiKey.Status = StatusAPIKeyActive
		}
		needsUpdate = true
	}
	if input.ExtraQuota != nil {
		apiKey.ExtraQuota = *input.ExtraQuota
		needsUpdate = true
	}
	if input.ResetQuota != nil && *input.ResetQuota {
		apiKey.QuotaUsed = 0
		if apiKey.Status == StatusAPIKeyQuotaExhausted {
			apiKey.Status = StatusAPIKeyActive
		}
		needsUpdate = true
	}
	if input.ResetExtraQuota != nil && *input.ResetExtraQuota {
		apiKey.ExtraQuotaUsed = 0
		needsUpdate = true
	}
	if input.ClearExpiration {
		apiKey.ExpiresAt = nil
		if apiKey.Status == StatusAPIKeyExpired {
			apiKey.Status = StatusAPIKeyActive
		}
		needsUpdate = true
	} else if input.ExpiresAt != nil {
		apiKey.ExpiresAt = input.ExpiresAt
		if apiKey.Status == StatusAPIKeyExpired && time.Now().Before(*input.ExpiresAt) {
			apiKey.Status = StatusAPIKeyActive
		}
		needsUpdate = true
	}
	if input.RateLimit5h != nil {
		apiKey.RateLimit5h = *input.RateLimit5h
		needsUpdate = true
	}
	if input.RateLimit1d != nil {
		apiKey.RateLimit1d = *input.RateLimit1d
		needsUpdate = true
	}
	if input.RateLimit7d != nil {
		apiKey.RateLimit7d = *input.RateLimit7d
		needsUpdate = true
	}

	resetRateLimit := input.ResetRateLimitUsage != nil && *input.ResetRateLimitUsage
	if resetRateLimit {
		apiKey.Usage5h = 0
		apiKey.Usage1d = 0
		apiKey.Usage7d = 0
		apiKey.Window5hStart = nil
		apiKey.Window1dStart = nil
		apiKey.Window7dStart = nil
		needsUpdate = true
	}

	if !needsUpdate {
		return result, nil
	}

	if err := s.apiKeyRepo.Update(ctx, apiKey); err != nil {
		return nil, fmt.Errorf("update api key: %w", err)
	}
	if s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByKey(ctx, apiKey.Key)
	}
	if resetRateLimit && s.billingCacheService != nil {
		_ = s.billingCacheService.InvalidateAPIKeyRateLimit(ctx, apiKey.ID)
	}

	result.APIKey = apiKey
	return result, nil
}
