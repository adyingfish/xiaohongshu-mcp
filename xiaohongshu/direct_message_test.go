package xiaohongshu

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/xpzouying/xiaohongshu-mcp/humanize"
)

const dmTestID = "0123456789abcdef01234567"

func dmRequest() DirectMessageRequest {
	return DirectMessageRequest{UserID: dmTestID, ExpectedName: "测试收件人", Content: "你好😀", Confirm: true}
}

type fakeDirectMessagePage struct {
	calls            []string
	initial          directMessageState
	states           []directMessageState
	postSendError    error
	missingBeforeIDs bool
	checkErr         error
	sendErr          error
	sendPanic        bool
	navigations      int
	lastURL          string
	neverReady       bool
	afterSend        bool
	fill             string
	fillContext      func(context.Context) error
}

func (f *fakeDirectMessagePage) Navigate(ctx context.Context, url string) error {
	f.navigations++
	f.lastURL = url
	return ctx.Err()
}
func (f *fakeDirectMessagePage) WaitLoad(ctx context.Context) error { return ctx.Err() }
func (f *fakeDirectMessagePage) Fill(ctx context.Context, text string) error {
	if f.fillContext != nil {
		if err := f.fillContext(ctx); err != nil {
			return err
		}
	}
	f.calls = append(f.calls, "fill")
	f.fill = text
	return nil
}
func (f *fakeDirectMessagePage) Run(ctx context.Context, action string, r DirectMessageRequest, _ string) (directMessageState, error) {
	f.calls = append(f.calls, action)
	if err := ctx.Err(); err != nil {
		return directMessageState{}, err
	}
	switch action {
	case "snapshot":
		if f.neverReady {
			return directMessageState{Ready: true, UserID: r.UserID}, nil
		}
		return directMessageState{Ready: true, ConversationReady: true, UserID: r.UserID}, nil
	case "list":
		return f.initial, nil
	case "send":
		f.afterSend = true
		if f.missingBeforeIDs {
			return directMessageState{Submitted: true}, nil
		}
		if f.sendPanic {
			panic("connection lost")
		}
		return directMessageState{Submitted: true, BeforeIDs: []string{"old"}}, f.sendErr
	case "check":
		if !f.afterSend {
			return f.initial, f.checkErr
		}
		if f.postSendError != nil {
			return directMessageState{}, f.postSendError
		}
		if len(f.states) > 0 {
			s := f.states[0]
			if len(f.states) > 1 {
				f.states = f.states[1:]
			}
			return s, nil
		}
		return directMessageState{}, nil
	}
	return directMessageState{}, errors.New("unexpected action")
}
func dmAction(f *fakeDirectMessagePage) *DirectMessageAction {
	return &DirectMessageAction{page: f, pollInterval: time.Millisecond, openTimeout: 100 * time.Millisecond, ackTimeout: 5 * time.Millisecond, delay: func(context.Context, humanize.Action) {}}
}

func TestDirectMessageCancellationDuringPauseDoesNotSubmit(t *testing.T) {
	for _, phase := range []humanize.Action{humanize.Reading, humanize.AfterType, humanize.BeforeSubmit} {
		t.Run(string(phase), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f := &fakeDirectMessagePage{}
			a := dmAction(f)
			a.delay = func(_ context.Context, action humanize.Action) {
				if action == phase {
					cancel()
				}
			}
			result, err := a.Send(ctx, dmRequest())
			require.ErrorIs(t, err, context.Canceled)
			require.Nil(t, result)
			require.NotContains(t, f.calls, "send")
			if phase == humanize.Reading {
				require.Empty(t, f.fill)
			}
		})
	}
}

func TestDirectMessageRechecksAfterReading(t *testing.T) {
	for _, change := range []func(*fakeDirectMessagePage){
		func(f *fakeDirectMessagePage) { f.checkErr = errors.New("收件人变化") },
		func(f *fakeDirectMessagePage) { f.initial.Draft = "新草稿" },
		func(f *fakeDirectMessagePage) {
			f.initial.Outgoing = []directMessageItem{{MessageID: "new", Text: dmRequest().Content}}
		},
	} {
		f := &fakeDirectMessagePage{}
		a := dmAction(f)
		a.delay = func(_ context.Context, action humanize.Action) {
			if action == humanize.Reading {
				change(f)
			}
		}
		_, err := a.Send(context.Background(), dmRequest())
		require.Error(t, err)
		require.Empty(t, f.fill)
		require.NotContains(t, f.calls, "send")
	}
}

func TestDirectMessageCancellationAfterAcknowledgementPreservesSent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := &fakeDirectMessagePage{states: []directMessageState{{Outgoing: []directMessageItem{
		{MessageID: "new", StoreID: "1", Text: dmRequest().Content},
	}}}}
	a := dmAction(f)
	a.delay = func(_ context.Context, action humanize.Action) {
		if action == humanize.AfterInteract {
			require.True(t, f.afterSend)
			cancel()
		}
	}
	result, err := a.Send(ctx, dmRequest())
	require.NoError(t, err)
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.Equal(t, "sent", result.Status)
	require.True(t, *result.Sent)
}
func TestDirectMessageValidationBeforeBrowser(t *testing.T) {
	for _, change := range []func(*DirectMessageRequest){
		func(r *DirectMessageRequest) { r.Confirm = false }, func(r *DirectMessageRequest) { r.UserID = "昵称" },
		func(r *DirectMessageRequest) { r.ExpectedName = " " }, func(r *DirectMessageRequest) { r.Content = " \n " },
		func(r *DirectMessageRequest) { r.Content = strings.Repeat("😀", 501) }, func(r *DirectMessageRequest) { r.Content = string([]byte{255}) },
	} {
		r := dmRequest()
		change(&r)
		f := &fakeDirectMessagePage{}
		_, err := dmAction(f).Send(context.Background(), r)
		require.Error(t, err)
		require.Zero(t, f.navigations)
	}
	r := dmRequest()
	r.Content = strings.Repeat("😀", 500)
	require.NoError(t, r.Normalize(true))
	r.Content = "  第一行\r\n第二行  "
	require.NoError(t, r.Normalize(true))
	require.Equal(t, "第一行\n第二行", r.Content)
}
func TestDirectMessagePreviewDoesNotFillOrSend(t *testing.T) {
	f := &fakeDirectMessagePage{}
	r := dmRequest()
	r.Confirm = false
	result, err := dmAction(f).Preview(context.Background(), r)
	require.NoError(t, err)
	require.Equal(t, "preview", result.Status)
	require.Equal(t, r.Content, result.Content)
	require.False(t, *result.Sent)
	require.Equal(t, chatURL+"?openUid="+dmTestID, f.lastURL)
	require.NotContains(t, f.calls, "fill")
	require.NotContains(t, f.calls, "send")
}
func TestDirectMessagePreflightRejectsMismatchDraftAndDuplicate(t *testing.T) {
	for _, f := range []*fakeDirectMessagePage{
		{checkErr: errors.New("收件人不符")}, {initial: directMessageState{Draft: "其他草稿"}},
		{initial: directMessageState{Outgoing: []directMessageItem{{MessageID: "old", Text: dmRequest().Content}}}},
	} {
		_, err := dmAction(f).Send(context.Background(), dmRequest())
		require.Error(t, err)
		require.NotContains(t, f.calls, "send")
		require.Empty(t, f.fill)
	}
}
func TestDirectMessageAcknowledgement(t *testing.T) {
	r := dmRequest()
	msg := func(id, store string, pending, failed bool) directMessageItem {
		return directMessageItem{MessageID: id, StoreID: store, Text: r.Content, Pending: pending, Failed: failed}
	}
	for _, tt := range []struct {
		name   string
		states []directMessageState
		want   string
	}{
		{"ack", []directMessageState{{Outgoing: []directMessageItem{msg("new", "1", true, false)}}, {Outgoing: []directMessageItem{msg("new", "18446744073709551616", false, false)}}}, "sent"},
		{"failed", []directMessageState{{Outgoing: []directMessageItem{msg("new", "0", false, true)}}}, "failed"},
		{"old cannot acknowledge", []directMessageState{{Outgoing: []directMessageItem{msg("old", "3", false, false)}}}, "unknown"},
		{"pending", []directMessageState{{Outgoing: []directMessageItem{msg("new", "3", true, false)}}}, "unknown"},
		{"zero store", []directMessageState{{Outgoing: []directMessageItem{msg("new", "000", false, false)}}}, "unknown"},
		{"no store", []directMessageState{{Outgoing: []directMessageItem{msg("new", "", false, false)}}}, "unknown"},
		{"draft remains", []directMessageState{{Draft: r.Content, Outgoing: []directMessageItem{msg("new", "3", false, false)}}}, "unknown"},
		{"ambiguous", []directMessageState{{Outgoing: []directMessageItem{msg("one", "3", false, false), msg("two", "4", false, false)}}}, "unknown"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeDirectMessagePage{states: tt.states}
			result, err := dmAction(f).Send(context.Background(), r)
			require.NoError(t, err)
			require.Equal(t, tt.want, result.Status)
			require.Equal(t, r.Content, f.fill)
			sends := 0
			for _, call := range f.calls {
				if call == "send" {
					sends++
				}
			}
			require.Equal(t, 1, sends)
			if tt.want == "unknown" {
				require.Nil(t, result.Sent)
			} else {
				require.Equal(t, tt.want == "sent", *result.Sent)
			}
		})
	}
}
func TestDirectMessageSendExceptionsAreUnknown(t *testing.T) {
	for _, f := range []*fakeDirectMessagePage{{sendErr: errors.New("disconnected")}, {sendPanic: true}, {postSendError: errors.New("recipient changed")}, {missingBeforeIDs: true}} {
		result, err := dmAction(f).Send(context.Background(), dmRequest())
		require.NoError(t, err)
		require.Equal(t, "unknown", result.Status)
		require.Nil(t, result.Sent)
	}
}
func TestDirectMessageExistingIdenticalDraftNotInsertedTwice(t *testing.T) {
	f := &fakeDirectMessagePage{initial: directMessageState{Draft: dmRequest().Content}}
	_, err := dmAction(f).Send(context.Background(), dmRequest())
	require.NoError(t, err)
	require.NotContains(t, f.calls, "fill")
}
func TestDirectMessageCancellationDoesNotSubmit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := &fakeDirectMessagePage{}
	_, err := dmAction(f).Send(ctx, dmRequest())
	require.Error(t, err)
	require.NotContains(t, f.calls, "send")
}
func TestDirectMessageListCoverage(t *testing.T) {
	f := &fakeDirectMessagePage{initial: directMessageState{Ready: true}}
	result, err := dmAction(f).List(context.Background(), "")
	require.NoError(t, err)
	require.False(t, result.Complete)
	require.NotNil(t, result.Conversations)
}

func TestDirectMessageMissingConversationTimesOutBeforeFill(t *testing.T) {
	f := &fakeDirectMessagePage{neverReady: true}
	a := dmAction(f)
	a.openTimeout = 10 * time.Millisecond
	_, err := a.Send(context.Background(), dmRequest())
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Contains(t, err.Error(), "尚未填写或发送")
	require.Empty(t, f.fill)
	require.NotContains(t, f.calls, "send")
}

// 服务入口和动作嵌套后，长正文仍有完整预算，且短调用方期限不能被延长。
func TestDirectMessageLongInputAcrossServiceAndActionBudgets(t *testing.T) {
	for _, shortCaller := range []bool{false, true} {
		t.Run(fmt.Sprint(shortCaller), func(t *testing.T) {
			r := dmRequest()
			r.Content = strings.Repeat("文", 1000)
			parent := context.Background()
			if shortCaller {
				var cancel context.CancelFunc
				parent, cancel = context.WithTimeout(parent, 100*time.Millisecond)
				defer cancel()
			}
			ctx, cancel := DirectMessageSendContext(parent, r.Content)
			defer cancel()
			f := &fakeDirectMessagePage{states: []directMessageState{{Outgoing: []directMessageItem{{MessageID: "new", StoreID: "1", Text: r.Content}}}}}
			f.fillContext = func(fillCtx context.Context) error {
				deadline, ok := fillCtx.Deadline()
				require.True(t, ok)
				if shortCaller {
					want, _ := parent.Deadline()
					require.Equal(t, want, deadline)
					<-fillCtx.Done()
					return fillCtx.Err()
				}
				require.Greater(t, time.Until(deadline), 450*time.Second)
				return nil
			}
			result, err := dmAction(f).Send(ctx, r)
			if shortCaller {
				require.ErrorIs(t, err, context.DeadlineExceeded)
				require.Nil(t, result)
				require.NotContains(t, f.calls, "send")
			} else {
				require.NoError(t, err)
				require.Equal(t, "sent", result.Status)
			}
		})
	}
}
