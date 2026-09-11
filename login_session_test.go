package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/xpzouying/xiaohongshu-mcp/xiaohongshu"
)

type fakeLoginBrowser struct {
	mu                       sync.Mutex
	state                    xiaohongshu.LoginPageState
	saves, refreshes, closed int
	saveErr                  error
}

func (b *fakeLoginBrowser) ReadState(context.Context) (xiaohongshu.LoginPageState, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state, nil
}
func (b *fakeLoginBrowser) RefreshQrcode(context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refreshes++
	b.state = xiaohongshu.LoginPageState{Status: "verification_required", Img: "data:image/png;base64,new"}
	return nil
}
func (b *fakeLoginBrowser) SaveCookies() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.saves++
	return b.saveErr
}
func (b *fakeLoginBrowser) Close() { b.mu.Lock(); defer b.mu.Unlock(); b.closed++ }
func (b *fakeLoginBrowser) set(state string, img string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.state = xiaohongshu.LoginPageState{Status: state, Img: img}
}
func newFakeLogin(t *testing.T) (*loginSessions, *fakeLoginBrowser, func(context.Context) (loginBrowser, error)) {
	t.Helper()
	l := &loginSessions{}
	b := &fakeLoginBrowser{state: xiaohongshu.LoginPageState{Status: "awaiting_scan", Img: "first"}}
	t.Cleanup(func() { _ = l.reset(func() error { return nil }) })
	return l, b, func(context.Context) (loginBrowser, error) { return b, nil }
}
func TestLoginReusesSessionAndReturnsVerification(t *testing.T) {
	l, b, create := newFakeLogin(t)
	ctx := context.Background()
	initial, err := l.get(ctx, create)
	require.NoError(t, err)
	b.set("verification_required", "second")
	status, active, err := l.status(ctx)
	require.NoError(t, err)
	require.True(t, active)
	require.Equal(t, "second", status.Img)
	require.False(t, status.IsLoggedIn)
	again, err := l.get(ctx, func(context.Context) (loginBrowser, error) {
		t.Fatal("must not replace pending browser")
		return nil, nil
	})
	require.NoError(t, err)
	require.Equal(t, initial.SessionID, again.SessionID)
	require.Equal(t, "second", again.Img)
	require.Zero(t, b.saves)
	b.set("logged_in", "")
	done, _, err := l.status(ctx)
	require.NoError(t, err)
	require.True(t, done.IsLoggedIn)
	require.Equal(t, 1, b.saves)
	require.Equal(t, 1, b.closed)
}
func TestLoginRefreshesOnlyExpiredChallenge(t *testing.T) {
	l, b, create := newFakeLogin(t)
	ctx := context.Background()
	initial, err := l.get(ctx, create)
	require.NoError(t, err)
	b.set("verification_expired", "")
	status, _, err := l.status(ctx)
	require.NoError(t, err)
	require.Empty(t, status.Img)
	require.Zero(t, b.refreshes)
	next, err := l.get(ctx, create)
	require.NoError(t, err)
	require.Equal(t, initial.SessionID, next.SessionID)
	require.Equal(t, "data:image/png;base64,new", next.Img)
	require.Equal(t, 1, b.refreshes)
	_, err = l.get(ctx, create)
	require.NoError(t, err)
	require.Equal(t, 1, b.refreshes)
}
func TestLoginDoesNotReportSuccessBeforeCookiesSaved(t *testing.T) {
	l, b, create := newFakeLogin(t)
	ctx := context.Background()
	_, err := l.get(ctx, create)
	require.NoError(t, err)
	b.mu.Lock()
	b.saveErr = errors.New("disk full")
	b.mu.Unlock()
	b.set("logged_in", "")
	res, active, err := l.status(ctx)
	require.True(t, active)
	require.ErrorContains(t, err, "保存 cookies 失败")
	require.Nil(t, res)
	require.Zero(t, b.closed)
	b.mu.Lock()
	b.saveErr = nil
	b.mu.Unlock()
	res, _, err = l.status(ctx)
	require.NoError(t, err)
	require.True(t, res.IsLoggedIn)
}
func TestLoginResetPreventsLateCookieWrite(t *testing.T) {
	l, b, create := newFakeLogin(t)
	ctx := context.Background()
	_, err := l.get(ctx, create)
	require.NoError(t, err)
	removed := false
	require.NoError(t, l.reset(func() error { removed = true; return nil }))
	require.True(t, removed)
	b.set("logged_in", "")
	_, active, err := l.status(ctx)
	require.NoError(t, err)
	require.False(t, active)
	require.Zero(t, b.saves)
	require.Equal(t, 1, b.closed)
}
func TestLoginExpiredAttemptDoesNotSaveAndConcurrentGetsReuse(t *testing.T) {
	l, b, create := newFakeLogin(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := l.get(ctx, create)
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, uint64(1), l.seq)
	l.mu.Lock()
	l.active.deadline = time.Now().Add(-time.Second)
	l.mu.Unlock()
	b.set("logged_in", "")
	status, _, err := l.status(ctx)
	require.NoError(t, err)
	require.Equal(t, "expired", status.Status)
	require.False(t, status.IsLoggedIn)
	require.Zero(t, b.saves)
}
func TestLoginMCPReturnsVerificationImageAndNeverExpiredImage(t *testing.T) {
	r := loginProgressResult(&LoginQrcodeResponse{Status: "verification_required", Message: "请扫码完成二次身份验证", Img: "data:image/png;base64,second", SessionID: 2})
	require.Len(t, r.Content, 2)
	require.Contains(t, r.Content[0].Text, "二次身份验证")
	require.Equal(t, "second", r.Content[1].Data)
	r = loginProgressResult(&LoginQrcodeResponse{Status: "verification_expired", Img: "data:image/png;base64,stale"})
	require.Len(t, r.Content, 1)
}
