package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

func (h *AccountHandler) SetVM2APIBridge(s *service.VM2APIBridge) { h.vm2apiBridge = s }
func (h *AccountHandler) VM2APIStatus(c *gin.Context) {
	id, e := strconv.ParseInt(c.Param("id"), 10, 64)
	if e != nil || id < 1 {
		response.BadRequest(c, "invalid account id")
		return
	}
	if h.vm2apiBridge == nil || !h.vm2apiBridge.Enabled() {
		response.Success(c, gin.H{"enabled": false})
		return
	}
	st, e := h.vm2apiBridge.State(c.Request.Context(), id)
	if e != nil {
		response.BadRequest(c, e.Error())
		return
	}
	st.Cleanup = nil
	response.Success(c, gin.H{"enabled": true, "job": st})
}
func (h *AccountHandler) VM2APISync(c *gin.Context) {
	id, e := strconv.ParseInt(c.Param("id"), 10, 64)
	if e != nil || id < 1 {
		response.BadRequest(c, "invalid account id")
		return
	}
	var body struct {
		InheritGroups bool `json:"inherit_groups"`
		Confirm       bool `json:"confirm"`
	}
	if c.ShouldBindJSON(&body) != nil || !body.Confirm {
		response.BadRequest(c, "请确认测试通过后关闭原账号")
		return
	}
	if h.vm2apiBridge == nil {
		response.BadRequest(c, "vm2api 集成尚未配置")
		return
	}
	st, e := h.vm2apiBridge.Start(c.Request.Context(), id, body.InheritGroups)
	if e != nil {
		response.Error(c, http.StatusConflict, e.Error())
		return
	}
	response.Success(c, st)
}
