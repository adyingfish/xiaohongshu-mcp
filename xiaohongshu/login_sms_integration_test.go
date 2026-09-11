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

func TestLoginSMSBrowserFixture(t *testing.T) {
	bin := os.Getenv("XHS_TEST_BROWSER")
	if bin == "" {
		t.Skip("set XHS_TEST_BROWSER for offline browser test")
	}
	l := launcher.New().Bin(bin).Headless(true).NoSandbox(true)
	url := l.MustLaunch()
	defer l.Cleanup()
	b := rod.New().ControlURL(url)
	require.NoError(t, b.Connect())
	defer b.MustClose()
	p := b.MustPage("about:blank")
	defer p.MustClose()
	router := p.HijackRequests()
	defer router.MustStop()
	router.MustAdd("*", func(h *rod.Hijack) {
		h.Response.SetHeader("Content-Type", "text/html; charset=utf-8").SetBody(`<!doctype html><body>
 <section class="login-container">手机号登录<input placeholder="输入手机号"><input id="normal-code" placeholder="输入验证码"><button onclick="window.wrongClicks++">登录</button></section>
 <section id="sms"><h3>短信验证码验证</h3><p>验证码已发送至 +86 151******15</p><input id="sms-code" placeholder="请输入验证码"><span>没有收到验证码？</span><a onclick="window.resends++">获取验证码</a><div class="submit btn-disabled" role="button" disabled="true">验证</div><span class="error"></span></section>
 <script>window.submits=0;window.wrongClicks=0;window.resends=0;window.inputEvents=0;document.querySelector('#sms-code').addEventListener('input',()=>{window.inputEvents++;setTimeout(()=>{const btn=document.querySelector('.submit');btn.setAttribute('disabled','false');btn.classList.remove('btn-disabled');},80);});document.querySelector('.submit').onclick=()=>{window.submits++;window.received=document.querySelector('#sms-code').value;document.querySelector('.error').textContent='验证码错误';};</script>
 </body>`)
	})
	go router.Run()
	p.MustNavigate("https://www.xiaohongshu.com/login").MustWaitLoad()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	a := NewLogin(p)
	state, err := a.ReadState(ctx)
	require.NoError(t, err)
	require.Equal(t, "sms_required", state.Status)
	require.Contains(t, state.Message, "151******15")
	require.Empty(t, state.Img)
	require.ErrorIs(t, a.SubmitSMSCode(ctx, "invalid"), ErrSMSNotSubmitted)
	require.Zero(t, p.MustEval(`() => window.submits`).Int())
	require.NoError(t, a.SubmitSMSCode(ctx, "012345"))
	require.Equal(t, 1, p.MustEval(`() => window.submits`).Int())
	require.Equal(t, "012345", p.MustEval(`() => window.received`).String())
	require.Equal(t, 1, p.MustEval(`() => window.inputEvents`).Int())
	require.Empty(t, p.MustEval(`() => document.querySelector('#normal-code').value`).String())
	require.Zero(t, p.MustEval(`() => window.wrongClicks+window.resends`).Int())
	state, err = a.ReadState(ctx)
	require.NoError(t, err)
	require.Equal(t, "sms_error", state.Status)
	require.NotContains(t, state.Message, "012345")
	p.MustEval(`() => document.querySelector('.submit').setAttribute('aria-disabled','true')`)
	require.ErrorIs(t, a.SubmitSMSCode(ctx, "654321"), ErrSMSNotSubmitted)
	require.Equal(t, 1, p.MustEval(`() => window.submits`).Int())
	p.MustEval(`() => document.querySelector('#sms').remove()`)
	state, err = a.ReadState(ctx)
	require.NoError(t, err)
	require.NotEqual(t, "sms_required", state.Status)
	require.ErrorIs(t, a.SubmitSMSCode(ctx, "654321"), ErrSMSNotSubmitted)
}
