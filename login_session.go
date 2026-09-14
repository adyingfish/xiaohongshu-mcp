package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/sirupsen/logrus"
	"github.com/xpzouying/xiaohongshu-mcp/xiaohongshu"
)

type loginBrowser interface {
	ReadState(context.Context) (xiaohongshu.LoginPageState, error)
	RefreshQrcode(context.Context) error
	SaveCookies() error
	Close()
}

type browserLogin struct {
	*xiaohongshu.LoginAction
	page  *rod.Page
	close func()
}

func (b *browserLogin) SaveCookies() error { return saveCookies(b.page) }
func (b *browserLogin) Close()             { b.close() }

func newLoginBrowser(ctx context.Context) (loginBrowser, error) {
	b := newBrowser()
	success := false
	defer func() {
		if !success {
			b.Close()
		}
	}()
	page := b.NewPage()
	action := xiaohongshu.NewLogin(page)
	if err := action.OpenLoginPage(ctx); err != nil {
		return nil, err
	}
	success = true
	return &browserLogin{LoginAction: action, page: page, close: func() { _ = page.Close(); b.Close() }}, nil
}

type loginAttempt struct {
	browser          loginBrowser
	seq              uint64
	deadline         time.Time
	verificationSeen bool
	smsID            string
	smsPending       bool
	smsUnknown       bool
	usedCodes        map[[32]byte]bool
	cancel           context.CancelFunc
}

// One lock serializes all browser access and cookie writes with reset/expiry.
// Polling and requesting a QR share this attempt; neither creates a second browser.
type loginSessions struct {
	mu     sync.Mutex
	seq    uint64
	active *loginAttempt
}

func (l *loginSessions) closeLocked() {
	if a := l.active; a != nil {
		l.active = nil
		a.cancel()
		a.browser.Close()
	}
}

func (l *loginSessions) reset(remove func() error) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closeLocked()
	return remove()
}

func (l *loginSessions) readLocked(ctx context.Context) (*LoginQrcodeResponse, error) {
	a := l.active
	if time.Now().After(a.deadline) {
		l.closeLocked()
		return &LoginQrcodeResponse{Status: "expired", Message: "登录会话已超时，请重新获取登录二维码。", Timeout: "0s"}, nil
	}
	state, err := a.browser.ReadState(ctx)
	if err != nil {
		return nil, err
	}
	if (state.Status == "verification_required" || state.Status == "verification_expired") && !a.verificationSeen {
		a.verificationSeen = true
		a.deadline = time.Now().Add(4 * time.Minute)
		logrus.Infof("登录会话 #%d 需要用户扫码完成二次身份验证", a.seq)
	}
	if state.Status == "sms_required" || state.Status == "sms_error" {
		if a.smsID == "" {
			var randomID [16]byte
			if _, err := rand.Read(randomID[:]); err != nil {
				return nil, fmt.Errorf("无法创建短信验证标识")
			}
			a.smsID = hex.EncodeToString(randomID[:])
			a.usedCodes = make(map[[32]byte]bool)
			a.deadline = time.Now().Add(4 * time.Minute)
			logrus.Infof("登录会话 #%d 需要用户提供短信验证码", a.seq)
		}
		if state.Status == "sms_error" {
			a.smsPending = false
			a.smsUnknown = false
		}
	}
	if a.smsPending && (state.Status == "sms_required" || state.Status == "awaiting_scan" || state.Status == "awaiting_confirmation") {
		state.Status = "sms_submitted"
		state.Img = ""
		state.Message = "已点击短信验证按钮，正在等待网页登录结果。请调用 check_login_status，不要重复提交验证码。"
		if a.smsUnknown {
			state.Status = "sms_submit_unknown"
			state.Message = "无法确认短信验证按钮是否已点击。请调用 check_login_status 检查结果，不要重复提交验证码；会话超时后重新登录。"
		}
	}
	res := &LoginQrcodeResponse{Status: state.Status, Message: state.Message, Img: state.Img, Timeout: time.Until(a.deadline).Round(time.Second).String(), SessionID: a.seq, VerificationID: a.smsID}
	if state.Status == "logged_in" {
		// The browser is authoritative only after persistence succeeds.
		if err := a.browser.SaveCookies(); err != nil {
			// Preserve this browser so a subsequent poll can retry the local disk write.
			logrus.Errorf("扫码成功但保存 cookies 失败，会话 #%d: %v", a.seq, err)
			return nil, fmt.Errorf("网页登录成功，但保存 cookies 失败: %w", err)
		}
		res.IsLoggedIn = true
		res.Timeout = "0s"
		logrus.Infof("扫码登录成功，cookies 已保存，会话 #%d", a.seq)
		l.closeLocked()
	}
	return res, nil
}

func (l *loginSessions) status(ctx context.Context) (*LoginQrcodeResponse, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.active == nil {
		return nil, false, nil
	}
	res, err := l.readLocked(ctx)
	return res, true, err
}

func (l *loginSessions) get(ctx context.Context, create func(context.Context) (loginBrowser, error)) (*LoginQrcodeResponse, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.active != nil && time.Now().After(l.active.deadline) {
		l.closeLocked()
	}
	if l.active == nil {
		initCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		b, err := create(initCtx)
		if err != nil {
			return nil, err
		}
		l.seq++
		bg, cancelBG := context.WithCancel(context.Background())
		a := &loginAttempt{browser: b, seq: l.seq, deadline: time.Now().Add(4 * time.Minute), cancel: cancelBG}
		l.active = a
		logrus.Infof("等待扫码登录，会话 #%d，超时 4m0s", a.seq)
		go l.observe(bg, a)
	}
	res, err := l.readLocked(ctx)
	if err != nil || l.active == nil {
		return res, err
	}
	if res.Status == "verification_expired" || res.Status == "qr_expired" {
		a := l.active
		if err := a.browser.RefreshQrcode(ctx); err != nil {
			return nil, err
		}
		// Do not hand out the expired image while the refresh request is in flight.
		deadline := time.NewTimer(8 * time.Second)
		defer deadline.Stop()
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-deadline.C:
				return res, nil
			case <-ticker.C:
				res, err = l.readLocked(ctx)
				if err != nil || l.active == nil || (res.Status != "verification_expired" && res.Status != "qr_expired") {
					return res, err
				}
			}
		}
	}
	return res, nil
}

func (l *loginSessions) observe(ctx context.Context, a *loginAttempt) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		l.mu.Lock()
		if l.active != a {
			l.mu.Unlock()
			return
		}
		_, err := l.readLocked(ctx)
		if err != nil && ctx.Err() == nil {
			logrus.Warnf("读取登录会话 #%d 状态失败: %v", a.seq, err)
		}
		l.mu.Unlock()
	}
}

// submitSMS never creates a login page: the code must belong to the active challenge.
func (l *loginSessions) submitSMS(ctx context.Context, sessionID uint64, verificationID, code string) (*LoginQrcodeResponse, error) {
	if !xiaohongshu.ValidLoginSMSCode(code) {
		return nil, fmt.Errorf("短信验证码须为 4 至 8 位数字")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	a := l.active
	if a == nil || sessionID != a.seq || verificationID == "" || verificationID != a.smsID {
		return nil, fmt.Errorf("短信验证码不属于当前登录会话，请先调用 check_login_status 获取当前验证标识")
	}
	state, err := l.readLocked(ctx)
	if err != nil {
		return nil, err
	}
	if l.active != a || (state.Status != "sms_required" && state.Status != "sms_error") || a.smsPending {
		return nil, fmt.Errorf("当前会话不在等待短信验证码，请检查登录状态，不要重复提交")
	}
	hash := sha256.Sum256([]byte(code))
	if a.usedCodes[hash] {
		return nil, fmt.Errorf("该验证码已提交过，不会重复发送，请检查登录状态")
	}
	browser, ok := a.browser.(interface {
		SubmitSMSCode(context.Context, string) error
	})
	if !ok {
		return nil, fmt.Errorf("当前浏览器不支持短信验证码提交")
	}
	// Reserve before browser interaction: uncertain results must not be retried automatically.
	a.usedCodes[hash] = true
	a.smsPending = true
	if err := browser.SubmitSMSCode(ctx, code); err != nil {
		if errors.Is(err, xiaohongshu.ErrSMSNotSubmitted) {
			// The browser guarantees it did not attempt a click. Keep the challenge usable.
			a.smsPending = false
			delete(a.usedCodes, hash)
		} else {
			// A lost browser response may follow a click. Preserve deduplication without claiming success.
			a.smsUnknown = true
		}
		return nil, err
	}
	return l.readLocked(ctx)
}
