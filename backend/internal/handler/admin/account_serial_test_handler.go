package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *AccountHandler) SerialTestPreset(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	var models *[]string
	if c.Request.Method == http.MethodPut {
		var req struct {
			Models []string `json:"models"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			response.BadRequest(c, "Invalid models")
			return
		}
		if err := service.ValidateSerialModels(req.Models); err != nil {
			response.BadRequest(c, err.Error())
			return
		}
		models = &req.Models
	}
	result, err := h.accountTestService.SerialTestPreset(c.Request.Context(), id, models)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "Failed to access test preset")
		return
	}
	response.Success(c, gin.H{"models": result})
}

func (h *AccountHandler) SerialTest(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	var req struct {
		Models []string `json:"models"`
	}
	if c.ShouldBindJSON(&req) != nil {
		response.BadRequest(c, "Invalid models")
		return
	}
	if err := service.ValidateSerialModels(req.Models); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if _, err := h.adminService.GetAccount(c.Request.Context(), id); err != nil {
		response.NotFound(c, "Account not found")
		return
	}
	release, ok := h.accountTestService.AcquireSerialTest(id)
	if !ok {
		response.Error(c, http.StatusConflict, "This account already has a serial test running")
		return
	}
	defer release()
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	emit := func(kind string, data any) {
		payload, _ := json.Marshal(gin.H{"type": kind, "data": data})
		_, _ = fmt.Fprintf(c.Writer, "data: %s\n\n", payload)
		c.Writer.Flush()
	}
	if err := h.accountTestService.RunSerialTest(c.Request.Context(), id, req.Models, emit); err != nil {
		emit("error", "Test stopped: unable to persist results")
		_ = c.Error(err)
	}
}
