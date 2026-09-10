package main

import (
	"context"
	"errors"

	"github.com/xpzouying/xiaohongshu-mcp/xiaohongshu"
)

func (s *XiaohongshuService) ListDirectMessageConversations(ctx context.Context, name string) (*xiaohongshu.DirectMessageConversationList, error) {
	b := newBrowser()
	defer b.Close()
	page := b.NewPage()
	defer page.Close()
	return xiaohongshu.NewDirectMessageAction(page).List(ctx, name)
}
func (s *XiaohongshuService) PreviewDirectMessage(ctx context.Context, r xiaohongshu.DirectMessageRequest) (*xiaohongshu.DirectMessageResult, error) {
	if err := r.Normalize(false); err != nil {
		return nil, err
	}
	b := newBrowser()
	defer b.Close()
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
	b := newBrowser()
	defer b.Close()
	page := b.NewPage()
	defer page.Close()
	return xiaohongshu.NewDirectMessageAction(page).Send(ctx, r)
}
