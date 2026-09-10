# 单人文字私信

通过 MCP 自己管理的浏览器和现有 Cookie 登录态操作小红书网页消息页面。支持会话查询、收件人与正文预览、发送一条文字私信；无需安装 XHS Bridge。

## MCP 工具与 HTTP 接口

| MCP 工具 | HTTP 接口 | 参数 |
| --- | --- | --- |
| `list_direct_message_conversations` | `GET /api/v1/direct-messages/conversations?name=完整昵称` | 可选 `name`，精确匹配完整昵称 |
| `preview_direct_message` | `POST /api/v1/direct-messages/preview` | `user_id`、`expected_name`、`content` |
| `send_direct_message` | `POST /api/v1/direct-messages/send` | 同上，加 `confirm: true` |

接口继承现有 `AUTH_TOKEN` / `-token` 鉴权配置。`user_id` 是主页或会话中的 24 位用户 ID，不是昵称或小红书号。查询仅覆盖网页当前加载的单人会话，`complete: false` 不代表没有更多会话。陌生人文件夹、群聊、图片、视频及文件消息不在本功能范围。

建议先查询收件人，再预览，取得用户对收件人与正文的发送授权后发送。预览不会填写或发送，也不会保存跨请求草稿；发送会重新核对用户 ID、会话类型、完整昵称、正文和输入框状态。打开消息页或会话可能被网站自动标记已读，返回 `read_may_mark_seen: true`。

预览示例：

```json
{
  "user_id": "0123456789abcdef01234567",
  "expected_name": "收件人完整昵称",
  "content": "你好，这是需要先预览的正文。"
}
```

获得该收件人与正文的发送授权后，向发送接口提交相同参数并增加 `"confirm": true`。也可以在已有授权时直接调用发送接口，不要求先调用预览。缺少确认会在启动浏览器之前拒绝。

## 结果判定

HTTP 结果沿用 `success` / `data` / `message` 外层结构；MCP 工具的文本内容为以下结果的 JSON。MCP 在 `failed` 和 `unknown` 时设置 `isError: true`。HTTP 对已进入发送阶段的 `failed` / `unknown` 返回 200，但顶层与 `data.success` 均为 false，调用方必须检查业务状态，不得根据 HTTP 200 判定发送成功。

| `status` | `success` | `sent` | 含义 |
| --- | --- | --- | --- |
| `preview` | true | false | 目标和正文已检查；正文通过 `content` 返回，未发送 |
| `sent` | true | true | 新的己方消息有非空 `message_id` 和正数 `store_id`，无 pending/failed 标记，输入框已清空 |
| `failed` | false | false | 网页显示这条新消息发送失败，未自动重试 |
| `unknown` | false | null | 发送尝试后发生超时、连接中断、会话变化或确认不足；可能已经发出 |

`verification: server_message_id` 表示网页取得服务端消息标识，不表示收件人已读。`failed` 和 `unknown` 都不能触发自动重发；先人工核对网页会话。不要为绕过重复保护修改正文。

正文统一 CRLF 换行并去除首尾空白，最多 1000 个 UTF-16 编码单元，部分表情占两个。已有不同草稿、引用消息、收件人不匹配、输入框不可用或最近一条己方消息正文相同时停止。相同草稿不会重复插入。

同一服务实例拒绝并发发送请求。此保护和最近消息去重不提供跨进程或跨重启的“恰好一次”保证；同一账号应避免多实例同时操作，调用方不得自动重试私信发送。

## 实现与验证

导航和文字填写使用 go-rod。页面脚本只负责读取一致的会话快照，以及在一次执行中核对收件人、正文和重复消息后触发 Enter。将核对与发送拆成两个浏览器调用会留下会话切换窗口，因此保留这段原子检查；没有调用私有发送 API。

网页状态判据适配自 `adyingfish/xiaohongshu-skills` 的 `scripts/xhs/direct_message.js`，保留原 MIT 版权及许可声明。MCP 使用独立请求浏览器，预览语义与 Skills 的填表草稿不同。

Go 单元测试会自动执行 Node 页面测试；本地缺少 Node.js 18+ 时明确跳过，CI 缺少 Node 时失败。验证命令：

```bash
go test ./...
go vet ./...
node xiaohongshu/direct_message.test.cjs
# 仅本地合成页面，拦截全部页面请求；不加载 Cookie，不访问真实账号。
DM_TEST_BROWSER=/absolute/path/to/chrome \
  go test -tags integration ./xiaohongshu -run '^TestDirectMessageBrowserFixture$' -v
# 全部集成测试只编译，不执行账号操作。
go test -tags integration -run '^$' ./...
```

这些检查验证参数、状态机、协议注册、页面身份检查及真实 Chromium 的输入／事件行为；不代表当前小红书线上页面或账号权限已实测可用。真实发送测试需要单独明确的收件人、正文和授权。
