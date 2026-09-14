//go:build integration

package xiaohongshu

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/stretchr/testify/require"
	"github.com/xpzouying/xiaohongshu-mcp/humanize"
)

type fastDirectMessageTyping struct{}

func (fastDirectMessageTyping) Timing() humanize.TimingProfile {
	profile := humanize.DefaultProvider{}.Timing()
	profile[humanize.Keystroke] = humanize.LogNormal{Min: time.Millisecond, Max: time.Millisecond}
	return profile
}

// 本地合成页面，所有页面请求均拦截；不加载账号 Cookie，不连接小红书。
func TestDirectMessageBrowserFixture(t *testing.T) {
	bin := os.Getenv("DM_TEST_BROWSER")
	if bin == "" {
		t.Skip("设置 DM_TEST_BROWSER 指向本地 Chromium 以运行离线浏览器测试")
	}
	l := launcher.New().Bin(bin).Headless(true).NoSandbox(true).Set("disable-background-networking")
	u, err := l.Launch()
	require.NoError(t, err)
	defer l.Cleanup()
	b := rod.New().ControlURL(u)
	require.NoError(t, b.Connect())
	defer b.MustClose()
	for _, mode := range []string{"sent", "failed", "pending", "wrong-name", "existing-draft", "changed-before-send", "preview", "list", "same-draft", "max-length", "recipient-during-pause", "draft-during-pause", "duplicate-during-pause", "cancel-during-input"} {
		t.Run(mode, func(t *testing.T) {
			page := b.MustPage("about:blank")
			defer page.MustClose()
			router := page.HijackRequests()
			defer router.MustStop()
			router.MustAdd("*", func(h *rod.Hijack) {
				h.Response.SetHeader("Content-Type", "text/html; charset=utf-8").SetBody(dmFixtureHTML(mode))
			})
			go router.Run()
			a := NewDirectMessageAction(page)
			a.pollInterval = 10 * time.Millisecond
			a.ackTimeout = 200 * time.Millisecond
			r := dmRequest()
			r.Content = "第一行\n第二行😀 <script>只是文字</script>"
			if mode == "same-draft" {
				r.Content = "已有相同草稿"
			}
			if mode == "max-length" {
				r.Content = strings.Repeat("文", 994) + "\n😀abc"
				humanize.SetProvider(fastDirectMessageTyping{})
				t.Cleanup(func() { humanize.SetProvider(humanize.DefaultProvider{}) })
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			// 在指定停顿阶段改变页面，验证延迟没有绕开发送前的原子检查。
			a.delay = func(_ context.Context, action humanize.Action) {
				if action == humanize.BeforeSubmit {
					switch mode {
					case "recipient-during-pause":
						page.MustEval(`() => document.querySelector('.xhs-im-chat-window__header-name').textContent = '其他收件人'`)
					case "draft-during-pause":
						page.MustEval(`() => document.querySelector('.xhs-im-input-bar-editor').textContent = '新草稿'`)
					case "duplicate-during-pause":
						page.MustEval(`text => {
							const item = document.createElement('div'); item.className = 'chat-item'; item.dataset.messageId = 'duplicate';
							const bubble = document.createElement('div'); bubble.className = 'chat-item__bubble--me';
							const content = document.createElement('div'); content.className = 'xhs-im-bubble__text'; content.textContent = text;
							bubble.append(content); item.append(bubble); document.querySelector('.xhs-im-msg-list').append(item);
						}`, r.Content)
					}
				}
				if mode == "cancel-during-input" && action == humanize.Reading {
					// 等到实际插入首字后取消，覆盖逐字输入中断而非仅预先取消。
					go func() {
						if page.Context(ctx).Wait(rod.Eval(`() => window.inputCount > 0`)) == nil {
							cancel()
						}
					}()
				}
			}
			switch mode {
			case "preview":
				res, err := a.Preview(ctx, r)
				require.NoError(t, err)
				require.Equal(t, "preview", res.Status)
				require.Empty(t, page.MustElement(directMessageEditor).MustText())
			case "list":
				res, err := a.List(ctx, r.ExpectedName)
				require.NoError(t, err)
				require.Len(t, res.Conversations, 1)
				require.Equal(t, r.UserID, res.Conversations[0].UserID)
			default:
				res, err := a.Send(ctx, r)
				if mode == "wrong-name" || mode == "existing-draft" || mode == "cancel-during-input" {
					require.Error(t, err)
					if mode == "cancel-during-input" {
						require.ErrorIs(t, err, context.Canceled)
						require.Positive(t, page.MustEval(`() => window.inputCount`).Int())
						require.Less(t, page.MustEval(`() => window.inputCount`).Int(), utf8.RuneCountInString(r.Content))
					}
				} else {
					require.NoError(t, err)
					want := mode
					if mode == "same-draft" || mode == "max-length" {
						want = "sent"
					}
					if mode == "pending" || mode == "changed-before-send" || strings.HasSuffix(mode, "-during-pause") {
						want = "unknown"
					}
					require.Equal(t, want, res.Status, "result: %+v", res)
				}
			}
			count := page.MustEval(`() => window.sentCount`).Int()
			if mode == "sent" || mode == "failed" || mode == "pending" || mode == "same-draft" || mode == "max-length" {
				require.Equal(t, 1, count)
				require.Equal(t, r.Content, page.MustEval(`() => window.submittedText`).Str())
				if mode != "same-draft" {
					require.Equal(t, utf8.RuneCountInString(r.Content), page.MustEval(`() => window.inputCount`).Int())
				}
			} else {
				require.Zero(t, count)
			}
			if mode == "same-draft" || mode == "preview" || mode == "list" || mode == "wrong-name" || mode == "existing-draft" {
				require.Zero(t, page.MustEval(`() => window.inputCount`).Int())
			}
		})
	}
}
func dmFixtureHTML(mode string) string {
	name := "测试收件人"
	if mode == "wrong-name" {
		name = "另一收件人"
	}
	store := "0"
	if mode == "sent" || mode == "same-draft" || mode == "max-length" {
		store = "1"
	}
	draft := ""
	if mode == "existing-draft" {
		draft = "用户尚未发送的草稿"
	}
	if mode == "same-draft" {
		draft = "已有相同草稿"
	}
	return fmt.Sprintf(`<!doctype html><meta charset="utf-8"><style>div{min-width:100px;min-height:20px}</style>
 <div class="xhs-im-view"><div class="xhs-im-conv-item xhs-im-conv-item--active" data-conv-kind="c2c" data-conv-id="%s"><div class="xhs-im-conv-item__name">%s</div></div>
 <div class="xhs-im-chat-window"><div class="xhs-im-chat-window__header-name">%s</div><div class="xhs-im-msg-list"></div><div class="xhs-im-input-bar-editor" contenteditable="true" data-placeholder="发消息...">%s</div></div></div>
 <script>window.sentCount=0;window.inputCount=0;const editor=document.querySelector('.xhs-im-input-bar-editor');
 editor.addEventListener('input',()=>{window.inputCount++;if('%s'==='changed-before-send')document.querySelector('.xhs-im-chat-window__header-name').textContent='错误收件人';});
 editor.addEventListener('keydown',e=>{if(e.key!=='Enter')return;e.preventDefault();window.sentCount++;
 window.submittedText=editor.innerText;
 const item=document.createElement('div');item.className='chat-item';item.dataset.messageId='new-message';item.dataset.storeId='%s';
 const bubble=document.createElement('div');bubble.className='chat-item__bubble--me';const text=document.createElement('div');text.className='xhs-im-bubble__text';text.textContent=editor.innerText;bubble.append(text);item.append(bubble);
 const mode='%s';if(mode==='failed'||mode==='pending'){const status=document.createElement('div');status.className=mode==='failed'?'chat-item__status-btn--failed':'chat-item__status-loading';item.append(status);}
 document.querySelector('.xhs-im-msg-list').append(item);editor.textContent='';});</script>`, dmTestID, name, name, draft, mode, store, mode)
}
