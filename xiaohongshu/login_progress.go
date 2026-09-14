package xiaohongshu

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

//go:embed login_state.js
var loginStateTemplate string

//go:embed login_sms.js
var loginSMSLocator string

var loginStateScript = strings.Replace(loginStateTemplate, "__SMS_LOCATOR__", loginSMSLocator, 1)

type LoginPageState struct {
	Status  string `json:"status"`
	Img     string `json:"img,omitempty"`
	Message string `json:"message,omitempty"`
}

// ReadState observes the existing page, without navigating away from a pending scan.
func (a *LoginAction) ReadState(ctx context.Context) (LoginPageState, error) {
	var state LoginPageState
	res, err := a.page.Context(ctx).Timeout(5 * time.Second).Eval(loginStateScript)
	if err != nil {
		return state, err
	}
	err = json.Unmarshal([]byte(res.Value.String()), &state)
	return state, err
}

// RefreshQrcode uses only the website's normal expired-QR refresh control.
// It never submits or solves the user's identity verification.
func (a *LoginAction) RefreshQrcode(ctx context.Context) error {
	res, err := a.page.Context(ctx).Timeout(5 * time.Second).Eval(`() => {
   const visible = el => el.getBoundingClientRect().width > 0 && el.getBoundingClientRect().height > 0;
   const imgs = [...document.querySelectorAll('img.qrcode-img')].filter(visible);
   for (const img of imgs) {
     for (let el = img.parentElement; el && el !== document.body; el = el.parentElement) {
       if (/扫码验证身份/.test(el.innerText) && /请通过验证|保护账号安全/.test(el.innerText) && el.querySelectorAll('img.qrcode-img').length === 1) {
         if (!/已过期|点击.*刷新/.test(el.innerText)) return false;
         img.click(); return true;
       }
     }
   }
   const login = [...document.querySelectorAll('.login-container')].find(visible);
   if (login && /二维码已过期/.test(login.innerText)) { login.querySelector('img.qrcode-img')?.click(); return true; }
   return false;
 }`)
	if err != nil {
		return err
	}
	if !res.Value.Bool() {
		return fmt.Errorf("当前页面没有可刷新的过期二维码")
	}
	return nil
}

func (a *LoginAction) OpenLoginPage(ctx context.Context) error {
	pp := a.page.Context(ctx)
	if err := pp.Navigate("https://www.xiaohongshu.com/explore"); err != nil {
		return err
	}
	// Wait for the login UI, not all unrelated page resources.
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		state, err := a.ReadState(ctx)
		if err == nil && (state.Img != "" || state.Status == "logged_in" || state.Status == "verification_required" || state.Status == "verification_expired" || state.Status == "sms_required" || state.Status == "sms_error") {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
