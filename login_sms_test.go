package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeSMSBrowser struct {
	*fakeLoginBrowser
	submits   int
	submitErr error
}

func (b *fakeSMSBrowser) SubmitSMSCode(context.Context, string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.submits++
	return b.submitErr
}
func setupSMS(t *testing.T) (*loginSessions, *fakeSMSBrowser, *LoginQrcodeResponse) {
	t.Helper()
	l, base, _ := newFakeLogin(t)
	b := &fakeSMSBrowser{fakeLoginBrowser: base}
	b.set("sms_required", "")
	progress, err := l.get(context.Background(), func(context.Context) (loginBrowser, error) { return b, nil })
	require.NoError(t, err)
	require.NotEmpty(t, progress.VerificationID)
	require.Equal(t, "sms_required", progress.Status)
	return l, b, progress
}
func TestSMSBindsToChallengeAndDoesNotEchoCode(t *testing.T) {
	l, b, p := setupSMS(t)
	ctx := context.Background()
	for _, args := range []SubmitLoginSMSArgs{{p.SessionID + 1, p.VerificationID, "012345"}, {p.SessionID, "stale", "012345"}, {p.SessionID, p.VerificationID, "not-a-code"}} {
		_, err := l.submitSMS(ctx, args.SessionID, args.VerificationID, args.Code)
		require.Error(t, err)
	}
	require.Zero(t, b.submits)
	result, err := l.submitSMS(ctx, p.SessionID, p.VerificationID, "012345")
	require.NoError(t, err)
	require.Equal(t, "sms_submitted", result.Status)
	require.False(t, result.IsLoggedIn)
	require.NotContains(t, loginProgressResult(result).Content[0].Text, "012345")
	_, err = l.submitSMS(ctx, p.SessionID, p.VerificationID, "012345")
	require.Error(t, err)
	require.Equal(t, 1, b.submits)
	b.set("logged_in", "")
	result, _, err = l.status(ctx)
	require.NoError(t, err)
	require.True(t, result.IsLoggedIn)
	require.Equal(t, 1, b.saves)
}
func TestSMSErrorAllowsNewUserCodeButNeverSameCode(t *testing.T) {
	l, b, p := setupSMS(t)
	ctx := context.Background()
	_, err := l.submitSMS(ctx, p.SessionID, p.VerificationID, "012345")
	require.NoError(t, err)
	b.set("sms_error", "")
	result, _, err := l.status(ctx)
	require.NoError(t, err)
	require.True(t, loginProgressResult(result).IsError)
	_, err = l.submitSMS(ctx, p.SessionID, p.VerificationID, "012345")
	require.Error(t, err)
	require.Equal(t, 1, b.submits)
	_, err = l.submitSMS(ctx, p.SessionID, p.VerificationID, "654321")
	require.NoError(t, err)
	require.Equal(t, 2, b.submits)
}
func TestSMSUnknownOutcomeAndConcurrentCallsSubmitOnce(t *testing.T) {
	l, b, p := setupSMS(t)
	b.submitErr = errors.New("submission outcome unknown")
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = l.submitSMS(context.Background(), p.SessionID, p.VerificationID, "012345")
		}()
	}
	wg.Wait()
	require.Equal(t, 1, b.submits)
	require.Zero(t, b.saves)
}
func TestSMSResetInvalidatesChallenge(t *testing.T) {
	l, b, p := setupSMS(t)
	require.NoError(t, l.reset(func() error { return nil }))
	_, err := l.submitSMS(context.Background(), p.SessionID, p.VerificationID, "012345")
	require.Error(t, err)
	require.Zero(t, b.submits)
	var next loginSessions
	defer next.reset(func() error { return nil })
	b2 := &fakeSMSBrowser{fakeLoginBrowser: &fakeLoginBrowser{}}
	b2.set("sms_required", "")
	p2, err := next.get(context.Background(), func(context.Context) (loginBrowser, error) { return b2, nil })
	require.NoError(t, err)
	require.NotEqual(t, p.VerificationID, p2.VerificationID)
	_, err = next.submitSMS(context.Background(), p.SessionID, p.VerificationID, "012345")
	require.Error(t, err)
	require.Zero(t, b2.submits)
}
func TestSMSMCPAndHTTPContracts(t *testing.T) {
	service := NewXiaohongshuService()
	base := &fakeLoginBrowser{}
	base.set("sms_required", "")
	b := &fakeSMSBrowser{fakeLoginBrowser: base}
	p, err := service.logins.get(context.Background(), func(context.Context) (loginBrowser, error) { return b, nil })
	require.NoError(t, err)
	defer service.logins.reset(func() error { return nil })
	router := setupRoutes(NewAppServer(service, "test-token"))
	call := func(path, body string, auth bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if auth {
			req.Header.Set("Authorization", "Bearer test-token")
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	args := SubmitLoginSMSArgs{SessionID: p.SessionID, VerificationID: p.VerificationID, Code: "012345"}
	raw, err := json.Marshal(args)
	require.NoError(t, err)
	require.Equal(t, 401, call("/api/v1/login/sms", string(raw), false).Code)
	require.Zero(t, b.submits)
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "submit_login_sms_code", "arguments": args}})
	require.NoError(t, err)
	res := call("/mcp", string(body), true)
	require.Equal(t, 200, res.Code)
	require.Contains(t, res.Body.String(), "sms_submitted")
	require.NotContains(t, res.Body.String(), "012345")
	require.Equal(t, 1, b.submits)
	res = call("/api/v1/login/sms", string(raw), true)
	require.Equal(t, 400, res.Code)
	require.NotContains(t, res.Body.String(), "012345")
	require.Equal(t, 1, b.submits)
}
