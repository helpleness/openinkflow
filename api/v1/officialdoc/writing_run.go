package officialdoc

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	commonResponse "InkFlow/model/common/response"
	request "InkFlow/model/officialdoc/request"
	service "InkFlow/service/officialdoc"
	"InkFlow/utils/ginctx"

	"github.com/gin-gonic/gin"
)

// WritingRunApi exposes the lifecycle of a durable MCP-controlled writing
// run. It intentionally has start/pause/resume/read operations only: users
// delete documents and versions through explicit manual management pages.
type WritingRunApi struct{}

func (api *WritingRunApi) Start(c *gin.Context) {
	taskID, ok := parseWritingRouteID(c, "id", "写作任务")
	if !ok {
		return
	}
	var req request.WritingRunCreate
	if err := c.ShouldBindJSON(&req); err != nil {
		commonResponse.BadRequest("MCP 写作运行参数不完整", c)
		return
	}
	item, err := service.ServiceGroupApp.WritingRunService.Start(c.Request.Context(), ginctx.CurrentTenantID(c), taskID, ginctx.CurrentUserID(c), req)
	commonResponse.Respond(item, err, commonResponse.ErrForbidden, c)
}

func (api *WritingRunApi) List(c *gin.Context) {
	taskID, ok := parseWritingRouteID(c, "id", "写作任务")
	if !ok {
		return
	}
	items, err := service.ServiceGroupApp.WritingRunService.List(c.Request.Context(), ginctx.CurrentTenantID(c), taskID, ginctx.CurrentUserID(c))
	commonResponse.Respond(items, err, commonResponse.ErrForbidden, c)
}

func (api *WritingRunApi) Get(c *gin.Context) {
	runID, ok := parseWritingRouteID(c, "id", "MCP 写作运行")
	if !ok {
		return
	}
	item, err := service.ServiceGroupApp.WritingRunService.Get(c.Request.Context(), ginctx.CurrentTenantID(c), runID, ginctx.CurrentUserID(c))
	commonResponse.Respond(item, err, commonResponse.ErrForbidden, c)
}

// Events keeps an SSE connection open for one durable writing run. The stream
// reads persisted messages and traces, so reconnecting after a browser or
// service restart continues from the same database-backed checkpoint.
func (api *WritingRunApi) Events(c *gin.Context) {
	runID, ok := parseWritingRouteID(c, "id", "MCP 写作运行")
	if !ok {
		return
	}
	tenantID := ginctx.CurrentTenantID(c)
	userID := ginctx.CurrentUserID(c)
	item, err := service.ServiceGroupApp.WritingRunService.Get(c.Request.Context(), tenantID, runID, userID)
	if err != nil {
		commonResponse.Respond(nil, err, commonResponse.ErrForbidden, c)
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	lastPayload := ""
	lastKeepAlive := time.Now()
	send := func(event string, payload any) {
		c.SSEvent(event, payload)
		c.Writer.Flush()
	}
	sendRun := func(run any) bool {
		encoded, marshalErr := json.Marshal(run)
		if marshalErr != nil || string(encoded) == lastPayload {
			return false
		}
		lastPayload = string(encoded)
		send("run", run)
		return true
	}
	completed := func(status string) bool {
		return status == "completed" || status == "failed" || status == "paused" || status == "canceled"
	}

	sendRun(item)
	if completed(item.Status) {
		send("done", item)
		return
	}

	ticker := time.NewTicker(700 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-ticker.C:
			item, err = service.ServiceGroupApp.WritingRunService.Get(c.Request.Context(), tenantID, runID, userID)
			if err != nil {
				send("error", gin.H{"message": err.Error()})
				return
			}
			sendRun(item)
			if completed(item.Status) {
				send("done", item)
				return
			}
			if time.Since(lastKeepAlive) >= 15*time.Second {
				send("ping", gin.H{"at": time.Now().UTC().Format(time.RFC3339)})
				lastKeepAlive = time.Now()
			}
		}
	}
}

func (api *WritingRunApi) Pause(c *gin.Context) {
	runID, ok := parseWritingRouteID(c, "id", "MCP 写作运行")
	if !ok {
		return
	}
	item, err := service.ServiceGroupApp.WritingRunService.Pause(c.Request.Context(), ginctx.CurrentTenantID(c), runID, ginctx.CurrentUserID(c))
	commonResponse.Respond(item, err, commonResponse.ErrForbidden, c)
}

func (api *WritingRunApi) Resume(c *gin.Context) {
	runID, ok := parseWritingRouteID(c, "id", "MCP 写作运行")
	if !ok {
		return
	}
	item, err := service.ServiceGroupApp.WritingRunService.Resume(c.Request.Context(), ginctx.CurrentTenantID(c), runID, ginctx.CurrentUserID(c))
	commonResponse.Respond(item, err, commonResponse.ErrForbidden, c)
}

func parseWritingRouteID(c *gin.Context, key, resource string) (uint, bool) {
	id, err := strconv.ParseUint(c.Param(key), 10, 64)
	if err != nil || id == 0 {
		commonResponse.BadRequest("无效的"+resource+" ID", c)
		return 0, false
	}
	return uint(id), true
}
