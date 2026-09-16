package main

import (
	"context"
	"errors"
	"github.com/sirupsen/logrus"

	"github.com/xpzouying/xiaohongshu-mcp/xiaohongshu"
)

func (s *XiaohongshuService) ListDirectMessageConversations(ctx context.Context, name string) (*xiaohongshu.DirectMessageConversationList, error) {
	ctx, cancel := context.WithTimeout(ctx, xiaohongshu.DirectMessageRequestTimeout)
	defer cancel()
	b := newBrowser()
	defer closeDirectMessageBrowser(b.Close)
	page := b.NewPage()
	defer page.Close()
	return xiaohongshu.NewDirectMessageAction(page).List(ctx, name)
}
func (s *XiaohongshuService) PreviewDirectMessage(ctx context.Context, r xiaohongshu.DirectMessageRequest) (*xiaohongshu.DirectMessageResult, error) {
	if err := r.Normalize(false); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, xiaohongshu.DirectMessageRequestTimeout)
	defer cancel()
	b := newBrowser()
	defer closeDirectMessageBrowser(b.Close)
	page := b.NewPage()
	defer page.Close()
	return xiaohongshu.NewDirectMessageAction(page).Preview(ctx, r)
}
func (s *XiaohongshuService) SendDirectMessage(ctx context.Context, r xiaohongshu.DirectMessageRequest) (*xiaohongshu.DirectMessageResult, error) {
	if err := r.Normalize(true); err != nil {
		return nil, err
	}
	// 拒绝同一服务的并发私信发送，避免两个请求同时通过重复检查。
	if !s.directMessageMu.TryLock() {
		return nil, errors.New("另一个私信发送请求正在处理；请先核对结果，不要自动重试")
	}
	defer s.directMessageMu.Unlock()
	ctx, cancel := xiaohongshu.DirectMessageSendContext(ctx, r.Content)
	defer cancel()
	b := newBrowser()
	defer closeDirectMessageBrowser(b.Close)
	page := b.NewPage()
	defer page.Close()
	result, err := xiaohongshu.NewDirectMessageAction(page).Send(ctx, r)
	if err != nil {
		// The action returns errors only before submit; submit always returns a
		// classified result, including transport failures with uncertain delivery.
		sent := false
		logrus.WithField("stage", "pre_submit_error").Info("direct_message_result")
		return &xiaohongshu.DirectMessageResult{Status: "failed", Sent: &sent, UserID: r.UserID, Recipient: r.ExpectedName, Stage: "pre_submit_error", Error: err.Error(), ReadMayMarkSeen: true}, nil
	}
	return result, nil
}

// 底层 Close 使用 MustClose；清理异常不能覆盖发送后已有的 sent/unknown 结果。
func closeDirectMessageBrowser(close func()) {
	defer func() { _ = recover() }()
	close()
}
