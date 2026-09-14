package xiaohongshu

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/go-rod/rod"
)

//go:embed direct_message.js
var directMessageScript string

const chatURL = "https://www.xiaohongshu.com/chat"

// Leave room for transport and browser cleanup before the client's 60-second timeout.
const DirectMessageRequestTimeout = 50 * time.Second
const directMessageEditor = ".xhs-im-chat-window .xhs-im-input-bar-editor"

var directMessageUserID = regexp.MustCompile(`^[0-9a-fA-F]{24}$`)
var positiveStoreID = regexp.MustCompile(`^[0-9]+$`)

// DirectMessageRequest 在预览和发送时使用相同的收件人与正文。
type DirectMessageRequest struct {
	UserID       string `json:"user_id" jsonschema:"收件人的24位用户ID，不是昵称或小红书号"`
	ExpectedName string `json:"expected_name" jsonschema:"收件人完整昵称，用于交叉核对"`
	Content      string `json:"content" jsonschema:"纯文字私信，最多1000个UTF-16编码单元"`
	Confirm      bool   `json:"confirm,omitempty" jsonschema:"发送必须为true，表示用户已授权此收件人和正文；预览无需确认"`
}

// Normalize 在连接浏览器之前校验输入，不让网页静默截断正文。
func (r *DirectMessageRequest) Normalize(send bool) error {
	r.UserID = strings.ToLower(r.UserID)
	r.Content = strings.TrimSpace(strings.ReplaceAll(r.Content, "\r\n", "\n"))
	if !directMessageUserID.MatchString(r.UserID) {
		return errors.New("user_id 必须是24位十六进制用户ID")
	}
	if strings.TrimSpace(r.ExpectedName) == "" {
		return errors.New("必须提供收件人的完整昵称 expected_name")
	}
	if !utf8.ValidString(r.Content) || r.Content == "" {
		return errors.New("私信必须是非空的 UTF-8 文字")
	}
	if len(utf16.Encode([]rune(r.Content))) > 1000 {
		return errors.New("私信超过1000个UTF-16编码单元，表情可能占两个")
	}
	if send && !r.Confirm {
		return errors.New("发送需要 confirm=true；仅预览请使用 preview_direct_message")
	}
	return nil
}

type DirectMessageConversation struct {
	UserID   string `json:"user_id"`
	Nickname string `json:"nickname"`
}

type DirectMessageConversationList struct {
	Conversations   []DirectMessageConversation `json:"conversations"`
	Scope           string                      `json:"scope"`
	Complete        bool                        `json:"complete"`
	ReadMayMarkSeen bool                        `json:"read_may_mark_seen"`
}

// Sent 为 nil 表示结果未知；sent 仅代表服务端确认，不代表收件人已读。
type DirectMessageResult struct {
	Success         bool   `json:"success"`
	Status          string `json:"status"`
	Sent            *bool  `json:"sent"`
	UserID          string `json:"user_id"`
	Recipient       string `json:"recipient"`
	Content         string `json:"content,omitempty"`
	MessageID       string `json:"message_id,omitempty"`
	StoreID         string `json:"store_id,omitempty"`
	Verification    string `json:"verification,omitempty"`
	Error           string `json:"error,omitempty"`
	ReadMayMarkSeen bool   `json:"read_may_mark_seen"`
}

type directMessageItem struct {
	MessageID string `json:"message_id"`
	StoreID   string `json:"store_id"`
	Text      string `json:"text"`
	Failed    bool   `json:"failed"`
	Pending   bool   `json:"pending"`
}

type directMessageState struct {
	OnChat            bool                        `json:"on_chat_page"`
	Ready             bool                        `json:"ready"`
	ConversationReady bool                        `json:"conversation_ready"`
	UserID            string                      `json:"user_id"`
	Recipient         string                      `json:"recipient"`
	Draft             string                      `json:"draft"`
	Outgoing          []directMessageItem         `json:"outgoing"`
	Conversations     []DirectMessageConversation `json:"conversations"`
	Submitted         bool                        `json:"submitted"`
	BeforeIDs         []string                    `json:"before_ids"`
	NotLoggedIn       bool                        `json:"not_logged_in"`
	Error             string                      `json:"error"`
}

// 小接口让协议状态机可以离线测试，实际导航和输入仍通过 go-rod。
type directMessagePage interface {
	Navigate(context.Context, string) error
	WaitLoad(context.Context) error
	Run(context.Context, string, DirectMessageRequest, string) (directMessageState, error)
	Fill(context.Context, string) error
}

type rodDirectMessagePage struct{ page *rod.Page }

func (p rodDirectMessagePage) Navigate(ctx context.Context, url string) error {
	page := p.page.Context(ctx)
	if err := page.Navigate(url); err != nil {
		return err
	}
	// Check the conversation identity separately; the caller retains the page-load barrier.
	return nil
}
func (p rodDirectMessagePage) WaitLoad(ctx context.Context) error {
	return p.page.Context(ctx).WaitLoad()
}
func (p rodDirectMessagePage) Run(ctx context.Context, action string, r DirectMessageRequest, name string) (directMessageState, error) {
	var state directMessageState
	res, err := p.page.Context(ctx).Eval(directMessageScript, map[string]any{
		"action": action, "user_id": r.UserID, "expected_name": r.ExpectedName, "content": r.Content, "name": name,
	})
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal([]byte(res.Value.JSON("", "")), &state); err != nil {
		return state, err
	}
	if state.NotLoggedIn {
		return state, errors.New("小红书未登录，请先扫码登录")
	}
	if state.Error != "" {
		return state, errors.New(state.Error)
	}
	return state, nil
}
func (p rodDirectMessagePage) Fill(ctx context.Context, text string) error {
	editor, err := p.page.Context(ctx).Element(directMessageEditor)
	if err != nil {
		return err
	}
	return editor.Input(text)
}

type DirectMessageAction struct {
	page         directMessagePage
	pollInterval time.Duration
	openTimeout  time.Duration
	ackTimeout   time.Duration
}

func NewDirectMessageAction(page *rod.Page) *DirectMessageAction {
	return &DirectMessageAction{page: rodDirectMessagePage{page}, pollInterval: 500 * time.Millisecond, openTimeout: 30 * time.Second, ackTimeout: 20 * time.Second}
}
func (a *DirectMessageAction) wait(ctx context.Context) error {
	timer := time.NewTimer(a.pollInterval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
func (a *DirectMessageAction) open(ctx context.Context, r DirectMessageRequest) (directMessageState, error) {
	ctx, cancel := context.WithTimeout(ctx, a.openTimeout)
	defer cancel()
	// The official profile button uses openUid to initialize a conversation even
	// when the recipient has no existing entry in the recent-conversation list.
	if err := a.page.Navigate(ctx, chatURL+"?openUid="+r.UserID); err != nil {
		return directMessageState{}, fmt.Errorf("打开私信会话失败（尚未填写或发送）: %w", err)
	}
	for {
		s, err := a.page.Run(ctx, "snapshot", r, "")
		if err != nil {
			return s, fmt.Errorf("检查私信会话失败（尚未填写或发送）: %w", err)
		}
		if s.Ready && s.UserID != "" && s.UserID != r.UserID {
			return s, errors.New("打开的私信会话不是目标收件人，尚未填写或发送")
		}
		if s.Ready && s.ConversationReady && s.UserID == r.UserID {
			// Preserve the existing load barrier before reading recent messages and
			// deciding whether the proposed text is a duplicate.
			if err := a.page.WaitLoad(ctx); err != nil {
				return s, fmt.Errorf("私信页面尚未完成加载（尚未填写或发送）: %w", err)
			}
			checked, err := a.page.Run(ctx, "check", r, "")
			if err != nil {
				return checked, fmt.Errorf("私信发送前检查失败（尚未填写或发送）: %w", err)
			}
			return checked, nil
		}
		if err := a.wait(ctx); err != nil {
			return s, fmt.Errorf("私信会话未就绪（尚未填写或发送）: %w", err)
		}
	}
}
func (a *DirectMessageAction) List(ctx context.Context, name string) (*DirectMessageConversationList, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if err := a.page.Navigate(ctx, chatURL); err != nil {
		return nil, err
	}
	// Keep the list's existing load barrier; opening a recipient instead waits
	// for its independently verified conversation controls.
	if err := a.page.WaitLoad(ctx); err != nil {
		return nil, err
	}
	for {
		state, err := a.page.Run(ctx, "list", DirectMessageRequest{}, name)
		if err != nil {
			return nil, err
		}
		if state.Ready {
			if state.Conversations == nil {
				state.Conversations = []DirectMessageConversation{}
			}
			return &DirectMessageConversationList{Conversations: state.Conversations, Scope: "loaded_single_conversations", Complete: false, ReadMayMarkSeen: true}, nil
		}
		if err := a.wait(ctx); err != nil {
			return nil, fmt.Errorf("私信列表未就绪: %w", err)
		}
	}
}
func validateDirectMessageDraft(s directMessageState, content string) error {
	if s.Draft != "" && s.Draft != content {
		return errors.New("当前会话已有不同草稿，未覆盖或发送")
	}
	if len(s.Outgoing) > 0 && s.Outgoing[len(s.Outgoing)-1].Text == content {
		return errors.New("最近一条己方私信与正文相同，请核对会话，勿重复发送")
	}
	return nil
}
func messageResult(r DirectMessageRequest, status string) *DirectMessageResult {
	sent := false
	return &DirectMessageResult{Status: status, Sent: &sent, UserID: r.UserID, Recipient: r.ExpectedName, ReadMayMarkSeen: true}
}
func (a *DirectMessageAction) Preview(ctx context.Context, r DirectMessageRequest) (*DirectMessageResult, error) {
	if err := r.Normalize(false); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	s, err := a.open(ctx, r)
	if err != nil {
		return nil, err
	}
	if err := validateDirectMessageDraft(s, r.Content); err != nil {
		return nil, err
	}
	result := messageResult(r, "preview")
	result.Success, result.Content = true, r.Content
	return result, nil
}
func (a *DirectMessageAction) Send(ctx context.Context, r DirectMessageRequest) (*DirectMessageResult, error) {
	if err := r.Normalize(true); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, DirectMessageRequestTimeout)
	defer cancel()
	s, err := a.open(ctx, r)
	if err != nil {
		return nil, err
	}
	if err := validateDirectMessageDraft(s, r.Content); err != nil {
		return nil, err
	}
	// 相同草稿不重复插入；不同草稿已拒绝。发送脚本会再次原子核对目标与正文。
	if s.Draft == "" {
		if err := a.page.Fill(ctx, r.Content); err != nil {
			return nil, err
		}
	}
	return a.submit(ctx, r), nil
}
func (a *DirectMessageAction) submit(ctx context.Context, r DirectMessageRequest) (result *DirectMessageResult) {
	result = messageResult(r, "unknown")
	result.Sent = nil
	result.Error = "未确认发送结果；请核对会话，勿直接重试"
	// 从尝试发送起，连接中断、页面切换和 panic 都不能被解释成可以重试。
	defer func() {
		if recover() != nil {
			result = messageResult(r, "unknown")
			result.Sent = nil
			result.Error = "发送期间异常，结果未知；请核对会话，勿直接重试"
		}
	}()
	submitted, err := a.page.Run(ctx, "send", r, "")
	if err != nil || !submitted.Submitted || submitted.BeforeIDs == nil {
		return result
	}
	before := make(map[string]bool, len(submitted.BeforeIDs))
	for _, id := range submitted.BeforeIDs {
		before[id] = true
	}
	ackCtx, cancel := context.WithTimeout(ctx, a.ackTimeout)
	defer cancel()
	for {
		state, err := a.page.Run(ackCtx, "check", r, "")
		if err != nil {
			return result
		}
		var candidates []directMessageItem
		for _, m := range state.Outgoing {
			if m.MessageID != "" && !before[m.MessageID] && m.Text == r.Content {
				candidates = append(candidates, m)
			}
		}
		if len(candidates) == 1 {
			m := candidates[0]
			if m.Failed {
				result = messageResult(r, "failed")
				result.MessageID = m.MessageID
				result.Error = "网页显示发送失败，未自动重试"
				return result
			}
			if positiveStoreID.MatchString(m.StoreID) && strings.TrimLeft(m.StoreID, "0") != "" && !m.Pending && state.Draft == "" {
				result = messageResult(r, "sent")
				*result.Sent = true
				result.Success = true
				result.MessageID, result.StoreID, result.Verification = m.MessageID, m.StoreID, "server_message_id"
				return result
			}
		}
		if err := a.wait(ackCtx); err != nil {
			return result
		}
	}
}
