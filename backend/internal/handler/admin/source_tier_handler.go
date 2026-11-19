package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *ScheduledTestHandler) SourceTierConfig(c *gin.Context) {
	if c.Request.Method == "PUT" {
		var v service.SourceTierConfig
		if err := c.ShouldBindJSON(&v); err != nil {
			response.BadRequest(c, err.Error())
			return
		}
		if err := h.gateway.SaveSourceTierConfig(c.Request.Context(), v); err != nil {
			response.BadRequest(c, err.Error())
			return
		}
	}
	v, err := h.gateway.SourceTierConfig(c.Request.Context())
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	c.JSON(200, v)
}
func (h *ScheduledTestHandler) SourceTierHealth(c *gin.Context) {
	var req struct {
		Config service.SourceTierConfig `json:"config"`
		Model  string                   `json:"model"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if req.Model == "" {
		response.BadRequest(c, "请选择模型")
		return
	}
	result, err := h.gateway.SourceTierHealth(c.Request.Context(), req.Config, req.Model)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	c.JSON(200, result)
}
