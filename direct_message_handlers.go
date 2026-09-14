package main

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/xpzouying/xiaohongshu-mcp/xiaohongshu"
)

type ListDirectMessageConversationsArgs struct {
	Name string `json:"name,omitempty" jsonschema:"可选，按完整昵称精确筛选已加载的单人会话；不读取消息正文"`
}

func directMessageToolResult(data any, err error) (*mcp.CallToolResult, any, error) {
	if err != nil {
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil, nil
	}
	body, err := json.Marshal(data)
	if err != nil {
		return nil, nil, err
	}
	failed := false
	if result, ok := data.(*xiaohongshu.DirectMessageResult); ok {
		failed = !result.Success
	}
	return &mcp.CallToolResult{IsError: failed, Content: []mcp.Content{&mcp.TextContent{Text: string(body)}}}, nil, nil
}

func registerDirectMessageTools(server *mcp.Server, app *AppServer) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_direct_message_conversations",
		Description: "列出网页已加载的单人私信会话及用户ID，可按完整昵称筛选；不保证全量，不含群聊。打开消息页可能自然标为已读。",
		Annotations: &mcp.ToolAnnotations{Title: "查询私信会话", DestructiveHint: boolPtr(false)},
	}, withPanicRecovery("list_direct_message_conversations", func(ctx context.Context, _ *mcp.CallToolRequest, args ListDirectMessageConversationsArgs) (*mcp.CallToolResult, any, error) {
		result, err := app.xiaohongshuService.ListDirectMessageConversations(ctx, args.Name)
		return directMessageToolResult(result, err)
	}))
	mcp.AddTool(server, &mcp.Tool{
		Name:        "preview_direct_message",
		Description: "打开已有或陌生人的单人私信会话，核对收件人ID、完整昵称、可发送状态与正文，返回预览；不填写或发送，不保留跨请求草稿。打开会话可能自然标为已读。",
		Annotations: &mcp.ToolAnnotations{Title: "预览私信", DestructiveHint: boolPtr(false)},
	}, withPanicRecovery("preview_direct_message", func(ctx context.Context, _ *mcp.CallToolRequest, args xiaohongshu.DirectMessageRequest) (*mcp.CallToolResult, any, error) {
		result, err := app.xiaohongshuService.PreviewDirectMessage(ctx, args)
		return directMessageToolResult(result, err)
	}))
	mcp.AddTool(server, &mcp.Tool{
		Name:        "send_direct_message",
		Description: "向已有会话或陌生人发送一条单人文字私信，必须先获得用户对收件人与正文的授权并设置confirm=true。核对ID与完整昵称，拒绝最近同文消息，仅发送一次。sent代表服务端确认，不代表已读；failed/unknown均不得自动重试，须先核对会话。",
		Annotations: &mcp.ToolAnnotations{Title: "发送私信", DestructiveHint: boolPtr(true), IdempotentHint: false},
	}, withPanicRecovery("send_direct_message", func(ctx context.Context, _ *mcp.CallToolRequest, args xiaohongshu.DirectMessageRequest) (*mcp.CallToolResult, any, error) {
		result, err := app.xiaohongshuService.SendDirectMessage(ctx, args)
		return directMessageToolResult(result, err)
	}))
}

func (s *AppServer) listDirectMessageConversationsHandler(c *gin.Context) {
	result, err := s.xiaohongshuService.ListDirectMessageConversations(c.Request.Context(), c.Query("name"))
	if err != nil {
		respondError(c, http.StatusInternalServerError, "DIRECT_MESSAGE_LIST_FAILED", "读取私信会话失败", err.Error())
		return
	}
	respondSuccess(c, result, "已读取当前加载的单人会话")
}
func (s *AppServer) previewDirectMessageHandler(c *gin.Context) { s.directMessageHandler(c, false) }
func (s *AppServer) sendDirectMessageHandler(c *gin.Context)    { s.directMessageHandler(c, true) }
func (s *AppServer) directMessageHandler(c *gin.Context, send bool) {
	var r xiaohongshu.DirectMessageRequest
	if err := c.ShouldBindJSON(&r); err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求参数错误", nil)
		return
	}
	if err := r.Normalize(send); err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_REQUEST", err.Error(), nil)
		return
	}
	var result *xiaohongshu.DirectMessageResult
	var err error
	if send {
		result, err = s.xiaohongshuService.SendDirectMessage(c.Request.Context(), r)
	} else {
		result, err = s.xiaohongshuService.PreviewDirectMessage(c.Request.Context(), r)
	}
	if err != nil {
		respondError(c, http.StatusBadRequest, "DIRECT_MESSAGE_REJECTED", err.Error(), nil)
		return
	}
	// unknown/failed 属于有结果的业务状态，不能用 5xx 诱发代理重试；顶层 success 同步实际结果。
	c.JSON(http.StatusOK, SuccessResponse{Success: result.Success, Data: result, Message: result.Status})
}
