# Fork 仓库协作约定

本文件约定个人 fork 的分支管理方式，供维护者和代码助手共同遵守。

## 远程仓库

- `origin`：个人 fork，`https://github.com/adyingfish/xiaohongshu-mcp.git`。个人分支推送到这里。
- `upstream`：原仓库，`https://github.com/xpzouying/xiaohongshu-mcp.git`。从这里获取更新，向这里提交通用功能和修复的 PR（拉取请求）。
- 创建 PR 时显式指定目标仓库和基础分支，避免把仅供个人使用的改动提交到原仓库。

## 分支职责

| 分支 | 用途 | 约定 |
| --- | --- | --- |
| `main` | 跟踪原仓库主分支 | 不直接提交个人改动，通过快进更新保持与 `upstream/main` 一致。 |
| `personal` | 日常安装和使用的版本 | 整合已验证的功能与修复；本文件等 fork 专用约定也放在这里。 |
| `feat/<功能名>` | 独立功能 | 通常从最新 `main` 创建，一个分支只解决一项需求，可向原仓库提 PR。 |
| `fix/<问题名>` | 独立修复 | 通常从最新 `main` 创建，复现问题并验证修复后单独提 PR。 |
| `docs/<主题>` | 文档修改 | 通用文档从 `main` 创建并面向原仓库；fork 专用文档从 `personal` 创建并面向个人 fork。 |

`personal` 初始版本基于与上游一致的 `main`。私信等独立功能通过 PR 合入 `personal`，审查通过且获得采用授权后才整合。不要自动整合待审查的 PR。

## 日常操作

### 1. 同步主分支

先检查工作区。存在未提交改动时保留其归属，使用独立工作树，或先妥善提交、保存；不要为切换分支而丢弃改动。

```bash
git status --short
git fetch upstream
git switch main
git merge --ff-only upstream/main
git push origin main
```

如果快进失败，先检查分叉提交及远程状态，不直接使用 `reset --hard` 或强制推送覆盖历史。

### 2. 开发独立功能或修复

同步 `main` 后，从它创建分支。以下命令中的功能名需要替换为实际名称：

```bash
git switch main
git switch -c feat/new-feature
# 完成修改、适当验证，并只提交本任务相关文件。
git push -u origin feat/new-feature
```

向原仓库提 PR 时，以 `upstream/main` 为基础，来源为个人 fork 中的该功能分支。不要从 `personal` 提交单项功能 PR，以免夹带其他个人改动。若确实依赖尚未合并的功能，明确说明依赖，并检查完整差异范围。

已有 PR 的后续修复继续提交到原功能分支，推送后 PR 会自动更新；无需为同一事项重复创建 PR。已发布的分支默认追加提交，需要重写历史时先确认使用者和影响范围。

### 3. 整合到自用版本

在确认要采用且验证通过后，将对应分支合入 `personal`：

```bash
git switch personal
git merge feat/new-feature
# 检查合并结果，运行与改动相关的验证。
git push origin personal
```

采用其他人的 PR 前，先获取其分支，在隔离目录检查相对当前版本的差异、兼容性和实际行为。能自动合并不等于功能可用；不要仅凭标题或原作者的测试结论整合。

上游更新进入 `main` 后，同样通过 `git merge main` 更新 `personal`，处理冲突并验证后再推送。保持自用分支稳定，不为整理历史频繁变基或强制推送。

日常安装使用的检出目录固定在 `personal`；需要同时开发其他功能时，使用单独工作树，避免切换分支改变正在使用的 MCP 版本。

### 4. 管理 fork 专用文档

本文件只维护在自用分支及其文档分支，不为了分发管理约定而修改镜像用途的 `main`，也不混入面向原仓库的功能 PR。

```bash
git switch personal
git switch -c docs/fork-conventions
# 修改并提交文档。
git push -u origin docs/fork-conventions
gh pr create --repo adyingfish/xiaohongshu-mcp \
  --base personal --head docs/fork-conventions
```

从干净的 `main` 创建的功能分支可能不含本文件；创建分支前先阅读这些约定，不为了携带约定而把个人整合分支合入功能分支。

### 5. 上游合并后的清理

- 功能被上游合并后，先同步 `main`，再更新并验证 `personal`。
- 上游可能使用压缩合并或改写实现；核对最终代码，不仅看提交编号或分支是否被标记为已合并。
- 确认功能已包含、没有未推送修改或其他依赖后，再删除对应本地及远程功能分支。
- 不批量删除 fork 中继承的分支，不强制覆盖未知提交。

## 提交与交付检查

- 操作前核对当前分支、远程地址及工作区状态，保留无关改动。
- 只暂存本次任务涉及的文件，提交前检查暂存差异。
- 按改动范围验证；纯文档修改检查文字、命令、链接和差异，无需运行无关的浏览器操作。
- 推送或创建 PR 后，核对线上目标分支、提交及文件列表，并向用户提供链接。
- 涉及小红书账号的验证沿用用户已授权的操作范围，不因分支管理或文档修改额外发送消息、关注或发布内容。

## 本 fork 的功能 PR

用户要求提交到个人 fork 时，显式使用 `--repo adyingfish/xiaohongshu-mcp --base personal --head feat/<功能名>`。功能分支从 `main` 创建，保持通用实现与 fork 专用文档分离；需要贡献上游时，另向 `xpzouying/xiaohongshu-mcp` 的 `main` 提 PR。创建 PR 不等于授权合并或发布。

私信验证使用离线测试及本地模拟页面；只有用户明确指定收件人和正文并授权发送时，才执行真实账号发送。不得沿用其他仓库或历史任务中的单次消息授权。
