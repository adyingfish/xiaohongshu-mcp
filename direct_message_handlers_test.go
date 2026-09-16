package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"github.com/xpzouying/xiaohongshu-mcp/xiaohongshu"
)

func TestDirectMessageRoutesAndValidation(t *testing.T) {
	router := setupRoutes(NewAppServer(NewXiaohongshuService(), ""))
	for _, path := range []string{"/api/v1/direct-messages/preview", "/api/v1/direct-messages/send"} {
		for _, body := range []string{`{}`, `{"user_id":"0123456789abcdef01234567","expected_name":"测试","content":""}`, `{"user_id":"wrong","expected_name":"测试","content":"你好"}`} {
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			require.Equal(t, 400, w.Code)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/direct-messages/send", strings.NewReader(`{"user_id":"0123456789abcdef01234567","expected_name":"测试","content":"你好"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, 400, w.Code)
	require.Contains(t, w.Body.String(), "confirm")
	for _, path := range []string{"/api/v1/direct-messages/conversations", "/api/v1/direct-messages/preview", "/api/v1/direct-messages/send"} {
		secured := setupRoutes(NewAppServer(NewXiaohongshuService(), "token"))
		method := "POST"
		if strings.HasSuffix(path, "conversations") {
			method = "GET"
		}
		w := httptest.NewRecorder()
		secured.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		require.Equal(t, 401, w.Code)
	}
}
func TestDirectMessageMCPRegistrationAndNoConfirmation(t *testing.T) {
	router := setupRoutes(NewAppServer(NewXiaohongshuService(), ""))
	post := func(body string) string {
		req := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		require.Equal(t, 200, w.Code)
		return w.Body.String()
	}
	body := post(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	for _, name := range []string{"list_direct_message_conversations", "preview_direct_message", "send_direct_message"} {
		require.Contains(t, body, name)
	}
	body = post(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"send_direct_message","arguments":{"user_id":"0123456789abcdef01234567","expected_name":"测试","content":"你好"}}}`)
	require.Contains(t, body, `"isError":true`)
	require.Contains(t, body, "confirm")
}
func TestDirectMessageUnknownToolResultIsError(t *testing.T) {
	for _, status := range []string{"unknown", "failed", "sent"} {
		sent := status == "sent"
		input := &xiaohongshu.DirectMessageResult{Status: status, Success: sent, Sent: &sent, Stage: "test_stage", Error: "测试原因"}
		if status == "unknown" {
			input.Sent = nil
		}
		result, _, err := directMessageToolResult(input, nil)
		require.NoError(t, err)
		require.Equal(t, status != "sent", result.IsError)
		var data map[string]any
		require.NoError(t, json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &data))
		require.Equal(t, status, data["status"])
		require.Equal(t, "测试原因", data["error"])
		require.Equal(t, "test_stage", data["stage"])
		if status == "unknown" {
			require.Nil(t, data["sent"])
		} else {
			require.Equal(t, sent, data["sent"])
		}
		structured, err := json.Marshal(result.StructuredContent)
		require.NoError(t, err)
		require.JSONEq(t, string(structured), result.Content[0].(*mcp.TextContent).Text)
	}
}
func TestDirectMessageConcurrentSendRejectedBeforeBrowser(t *testing.T) {
	s := NewXiaohongshuService()
	s.directMessageMu.Lock()
	defer s.directMessageMu.Unlock()
	_, err := s.SendDirectMessage(context.Background(), xiaohongshu.DirectMessageRequest{UserID: "0123456789abcdef01234567", ExpectedName: "测试", Content: "你好", Confirm: true})
	require.ErrorContains(t, err, "正在处理")
}

func TestDirectMessageCleanupPreservesResult(t *testing.T) {
	for _, status := range []string{"sent", "unknown"} {
		result := func() *xiaohongshu.DirectMessageResult {
			defer closeDirectMessageBrowser(func() { panic("browser disconnected during cleanup") })
			return &xiaohongshu.DirectMessageResult{Status: status, Success: status == "sent"}
		}()
		require.Equal(t, status, result.Status)
	}
}
