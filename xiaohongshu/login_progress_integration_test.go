//go:build integration

package xiaohongshu

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/stretchr/testify/require"
)

// All documents are local fixtures. No account data or Xiaohongshu requests.
func TestLoginVerificationBrowserFixture(t *testing.T) {
	bin := os.Getenv("XHS_TEST_BROWSER")
	if bin == "" {
		t.Skip("set XHS_TEST_BROWSER to run offline browser regression")
	}
	l := launcher.New().Bin(bin).Headless(true).NoSandbox(true)
	url := l.MustLaunch()
	defer l.Cleanup()
	b := rod.New().ControlURL(url)
	require.NoError(t, b.Connect())
	defer b.MustClose()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	p := b.MustPage("about:blank")
	defer p.MustClose()
	require.NoError(t, p.SetDocumentContent(`<html><body>
 <section class="login-container"><span>请扫码登录</span><img class="qrcode-img" style="width:128px;height:128px" src="data:image/png;base64,first"></section>
 <script>window.refreshes=0;</script></body></html>`))
	a := NewLogin(p)
	s, err := a.ReadState(ctx)
	require.NoError(t, err)
	require.Equal(t, "awaiting_scan", s.Status)
	require.Contains(t, s.Img, "first")
	p.MustEval(`() => document.querySelector('.login-container span').textContent='扫码成功 请在手机上确认'`)
	s, err = a.ReadState(ctx)
	require.NoError(t, err)
	require.Equal(t, "awaiting_confirmation", s.Status)
	require.Empty(t, s.Img)
	p.MustEval(`() => {
 const d=document.createElement('div');d.id='verify';d.innerHTML='<h2>请通过验证</h2><p>为保护账号安全，请使用已登录该账号的小红书APP扫码验证身份</p><img class="qrcode-img" style="width:174px;height:174px" src="data:image/png;base64,second"><p class="expires">二维码1分钟失效</p>';
 d.onclick=()=>{window.refreshes++;d.querySelector('img').src='data:image/png;base64,refreshed';d.querySelector('.expires').textContent='二维码1分钟失效'};document.body.append(d);
 }`)
	s, err = a.ReadState(ctx)
	require.NoError(t, err)
	require.Equal(t, "verification_required", s.Status)
	require.Contains(t, s.Img, "second")
	require.Contains(t, s.Message, "二次身份验证")
	require.Error(t, a.RefreshQrcode(ctx))
	require.Zero(t, p.MustEval(`() => window.refreshes`).Int())
	p.MustEval(`() => document.querySelector('#verify .expires').textContent='已过期，点击二维码区域刷新'`)
	s, err = a.ReadState(ctx)
	require.NoError(t, err)
	require.Equal(t, "verification_expired", s.Status)
	require.Empty(t, s.Img)
	require.NoError(t, a.RefreshQrcode(ctx))
	require.Equal(t, 1, p.MustEval(`() => window.refreshes`).Int())
	s, err = a.ReadState(ctx)
	require.NoError(t, err)
	require.Equal(t, "verification_required", s.Status)
	require.Contains(t, s.Img, "refreshed")
	// A stale/hidden initial QR or user sidebar never overrides an active challenge.
	p.MustEval(`() => {const x=document.createElement('div');x.className='main-container';x.innerHTML='<div class="user"><div class="link-wrapper"><a class="channel">我</a></div></div>';document.body.append(x)}`)
	s, err = a.ReadState(ctx)
	require.NoError(t, err)
	require.Equal(t, "verification_required", s.Status)
	p.MustEval(`() => {document.querySelector('#verify').remove();document.querySelector('.login-container').remove()}`)
	s, err = a.ReadState(ctx)
	require.NoError(t, err)
	require.Equal(t, "logged_in", s.Status)
	// Verification without an image must not fall back to the initial login QR.
	p.MustEval(`() => {const x=document.createElement('div');x.innerHTML='<p>请使用小红书APP扫码验证身份</p>';document.body.append(x)}`)
	s, err = a.ReadState(ctx)
	require.NoError(t, err)
	require.Equal(t, "verification_required", s.Status)
	require.Empty(t, s.Img)
}
