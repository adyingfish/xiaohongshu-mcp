//go:build integration

package xiaohongshu

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/stretchr/testify/require"
)

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
	for _, mode := range []string{"sent", "failed", "pending", "wrong-name", "existing-draft", "changed-before-send", "preview", "list"} {
		t.Run(mode, func(t *testing.T) {
			page := b.MustPage("about:blank")
			defer page.MustClose()
			router := page.HijackRequests()
			defer router.MustStop()
			router.MustAdd("*", func(h *rod.Hijack) {
				h.Response.SetHeader("Content-Type", "text/html; charset=utf-8").SetBody(strings.Replace(dmFixtureHTML(mode), "<script>", "<script>history.replaceState(null, '', '/chat/"+dmTestID+"');", 1))
			})
			go router.Run()
			a := NewDirectMessageAction(page)
			a.pollInterval = 10 * time.Millisecond
			a.ackTimeout = 200 * time.Millisecond
			r := dmRequest()
			r.Content = "第一行\n第二行😀 <script>只是文字</script>"
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
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
				if mode == "wrong-name" || mode == "existing-draft" {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
					want := mode
					if mode == "pending" || mode == "changed-before-send" {
						want = "unknown"
					}
					require.Equal(t, want, res.Status, "result: %+v", res)
				}
			}
			count := page.MustEval(`() => window.sentCount`).Int()
			if mode == "sent" || mode == "failed" || mode == "pending" {
				require.Equal(t, 1, count)
			} else {
				require.Zero(t, count)
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
	if mode == "sent" {
		store = "1"
	}
	draft := ""
	if mode == "existing-draft" {
		draft = "用户尚未发送的草稿"
	}
	return fmt.Sprintf(`<!doctype html><meta charset="utf-8"><style>div{min-width:100px;min-height:20px}</style>
 <div class="xhs-im-view"><div class="xhs-im-conv-item xhs-im-conv-item--active" data-conv-kind="c2c" data-conv-id="%s"><div class="xhs-im-conv-item__name">%s</div></div>
 <div class="xhs-im-chat-window"><div class="xhs-im-chat-window__header-name">%s</div><div class="xhs-im-msg-list"></div><div class="xhs-im-input-bar-editor" contenteditable="true" data-placeholder="发消息...">%s</div></div></div>
 <script>window.sentCount=0;const editor=document.querySelector('.xhs-im-input-bar-editor');
 editor.addEventListener('input',()=>{if('%s'==='changed-before-send')document.querySelector('.xhs-im-chat-window__header-name').textContent='错误收件人';});
 editor.addEventListener('keydown',e=>{if(e.key!=='Enter')return;e.preventDefault();window.sentCount++;
 const item=document.createElement('div');item.className='chat-item';item.dataset.messageId='new-message';item.dataset.storeId='%s';
 const bubble=document.createElement('div');bubble.className='chat-item__bubble--me';const text=document.createElement('div');text.className='xhs-im-bubble__text';text.textContent=editor.innerText;bubble.append(text);item.append(bubble);
 const mode='%s';if(mode==='failed'||mode==='pending'){const status=document.createElement('div');status.className=mode==='failed'?'chat-item__status-btn--failed':'chat-item__status-loading';item.append(status);}
 document.querySelector('.xhs-im-msg-list').append(item);editor.textContent='';});</script>`, dmTestID, name, name, draft, mode, store, mode)
}

// A new recipient has no row, header or editor until the website processes openUid.
// This fixture deliberately leaves a direct /chat/<id> navigation uninitialized.
func TestDirectMessageStrangerBrowserFixture(t *testing.T) {
	bin := os.Getenv("DM_TEST_BROWSER")
	if bin == "" {
		t.Skip("set DM_TEST_BROWSER for offline browser tests")
	}
	l := launcher.New().Bin(bin).Headless(true).NoSandbox(true).Set("disable-background-networking")
	u, err := l.Launch()
	require.NoError(t, err)
	defer l.Cleanup()
	b := rod.New().ControlURL(u)
	require.NoError(t, b.Connect())
	defer b.MustClose()
	for _, mode := range []string{"preview", "sent", "failed", "pending", "wrong-name", "disabled", "existing-draft", "changed-before-send"} {
		t.Run(mode, func(t *testing.T) {
			page := b.MustPage("about:blank")
			defer page.MustClose()
			router := page.HijackRequests()
			defer router.MustStop()
			router.MustAdd("*", func(h *rod.Hijack) {
				h.Response.SetHeader("Content-Type", "text/html; charset=utf-8").SetBody(dmStrangerFixtureHTML(mode))
			})
			go router.Run()
			a := NewDirectMessageAction(page)
			a.pollInterval = 10 * time.Millisecond
			a.openTimeout = time.Second
			a.ackTimeout = 200 * time.Millisecond
			r := dmRequest()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if mode == "preview" {
				res, err := a.Preview(ctx, r)
				require.NoError(t, err)
				require.Equal(t, "preview", res.Status)
				require.False(t, *res.Sent)
				require.Empty(t, page.MustElement(directMessageEditor).MustText())
			} else {
				res, err := a.Send(ctx, r)
				if mode == "wrong-name" || mode == "disabled" || mode == "existing-draft" {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
					want := mode
					if mode == "pending" || mode == "changed-before-send" {
						want = "unknown"
					}
					require.Equal(t, want, res.Status)
					if mode == "sent" {
						require.True(t, *res.Sent)
						require.Equal(t, "server_message_id", res.Verification)
					} else if want == "unknown" {
						require.Nil(t, res.Sent)
					}
				}
			}
			require.True(t, page.MustEval(`() => window.openedFromQuery`).Bool())
			require.False(t, page.MustEval(`() => window.targetBeforeOpen`).Bool())
			wantSends := 0
			if mode == "sent" || mode == "failed" || mode == "pending" {
				wantSends = 1
			}
			require.Equal(t, wantSends, page.MustEval(`() => window.sentCount`).Int())
		})
	}
}

func dmStrangerFixtureHTML(mode string) string {
	base := dmFixtureHTML(mode)
	start := strings.Index(base, `<div class="xhs-im-view">`)
	end := strings.Index(base, `<script>`)
	markup := base[start:end]
	handlers := strings.TrimSuffix(base[end+len(`<script>`):], `</script>`)
	return fmt.Sprintf(`<!doctype html><meta charset="utf-8"><style>div{min-width:100px;min-height:20px}</style>
<div id="loading">加载消息</div><template id="new-conversation">%s</template>
<script>
window.sentCount=0;window.openedFromQuery=false;window.targetBeforeOpen=!!document.querySelector('[data-conv-id]');
const ids=new URLSearchParams(location.search).getAll('openUid');
if(ids.length===1 && ids[0]==='%s')setTimeout(()=>{
 document.querySelector('#loading').remove();
 document.body.append(document.querySelector('#new-conversation').content.cloneNode(true));
 (function(){%s})();
 if('%s'==='disabled'){document.querySelector('.xhs-im-input-bar-editor').setAttribute('contenteditable','false');}
 window.openedFromQuery=true;
},80);
</script>`, markup, dmTestID, handlers, mode)
}
