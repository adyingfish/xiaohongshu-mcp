package main

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type SubmitLoginSMSArgs struct {
	SessionID      uint64 `json:"session_id" jsonschema:"check_login_status 返回的当前登录会话编号"`
	VerificationID string `json:"verification_id" jsonschema:"check_login_status 返回的当前短信验证标识，防止跨会话提交"`
	Code           string `json:"code" jsonschema:"用户明确提供的本次短信验证码，4至8位数字；不得猜测或复用历史验证码"`
}

func registerLoginSMSTool(server *mcp.Server, app *AppServer) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "submit_login_sms_code",
		Description: "仅当登录状态为 sms_required 或 sms_error 且用户明确提供本次短信验证码时调用。将验证码提交给同一浏览器中的小红书官方短信验证表单，不发送新短信。必须传入当前 session_id、verification_id 与用户提供的 code。不得猜测、自动重试或记录验证码；提交后继续检查登录状态，只有服务明确已登录才算成功。",
		Annotations: &mcp.ToolAnnotations{Title: "提交登录短信验证码", ReadOnlyHint: false, DestructiveHint: boolPtr(false), IdempotentHint: false},
	}, withPanicRecovery("submit_login_sms_code", func(ctx context.Context, _ *mcp.CallToolRequest, args SubmitLoginSMSArgs) (*mcp.CallToolResult, any, error) {
		result, err := app.xiaohongshuService.logins.submitSMS(ctx, args.SessionID, args.VerificationID, args.Code)
		if err != nil {
			return convertToMCPResult(&MCPToolResult{IsError: true, Content: []MCPContent{{Type: "text", Text: err.Error()}}}), nil, nil
		}
		return convertToMCPResult(loginProgressResult(result)), nil, nil
	}))
}
func (s *AppServer) submitLoginSMSHandler(c *gin.Context) {
	var args SubmitLoginSMSArgs
	if err := c.ShouldBindJSON(&args); err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_REQUEST", "短信验证参数格式错误", nil)
		return
	}
	result, err := s.xiaohongshuService.logins.submitSMS(c.Request.Context(), args.SessionID, args.VerificationID, args.Code)
	if err != nil {
		respondError(c, http.StatusBadRequest, "LOGIN_SMS_REJECTED", err.Error(), nil)
		return
	}
	respondSuccess(c, result, "已提交短信验证码，请继续检查登录状态")
}
