# CPA-Helper 0.3.37

This release adds bounded model-request admission and streaming safeguards without changing CLIProxyAPI / CPA.

## Model request backpressure

- Add configurable global concurrency, bounded queue length and queue timeout at the Helper model-request entry point.
- Reject full or expired queues before reading the request body with an OpenAI-compatible HTTP 429 response.
- Keep the 32 MB compatibility limit while spilling request bodies larger than 1 MB to permission-restricted temporary files.
- Extract the top-level model with a bounded streaming scanner so large JSON fields are not buffered back into memory.

## Streaming and auth pools

- Flush SSE response headers and every streamed chunk, and send `X-Accel-Buffering: no` for Nginx.
- Add a configurable total concurrency limit to each auth pool, independent of per-account limits.
- Add migration `202607210001` for admission settings and pool concurrency.

## Validation

- Backend: `go test ./...`, `go vet ./...`
- Frontend: `npm.cmd run build`
- Linux: amd64 and arm64 builds

## Previous 0.3.36

This release repairs unknown usage ownership without changing CLIProxyAPI / CPA.

## Usage ownership

- Read API-key ownership hashes from authenticated `cpa-auth-pool` scheduler and completion events.
- Match usage by auth ID, model, provider and request timing, with a one-second ambiguity guard for concurrent cross-user traffic.
- Propagate one verified owner across failed and successful provider attempts sharing the same CPA request ID.
- Repair recent historical unknown rows while collecting usage and before serving usage pages.
- Keep plugin event reads bounded, cached and limited to a three-second timeout.

## Validation

- Backend: `go test ./...`, `go vet ./...`
- Plugin: `go test ./...`, `go vet ./...`
- GitHub Actions: Linux amd64/arm64 release assets and multi-architecture container build

## Previous 0.3.35

本次发布完善号池调度策略、上游错误诊断以及限额账号自动切换。

## 号池调度

- 每个号池可独立选择“轮询均衡”或“优先填充”。
- 调度策略通过 Helper、数据库和 `cpa-auth-pool` 插件完整持久化并校验。
- 新增迁移 `202607180005`，现有号池默认保持轮询均衡。
- 渠道状态优先依据当前可用账号和最近请求判断，避免少量已耗尽账号导致整个号池误报降级。

## 错误诊断与换号

- 插件监控结构化展示模型不支持、SOCKS 代理异常、使用上限、频率限制和通用上游错误。
- 显示 HTTP 状态、套餐、额度重置时间、剩余时间以及经过脱敏的原始错误详情。
- HTTP 429 账号会进入持久化冷却，后续调度自动选择其他号池成员，并按上游返回的重置时间恢复。

## 验证

- 后端：`go test ./...`
- 前端：`npm.cmd run build`、`npm.cmd run lint`、`npm.cmd run test:i18n`
- 插件：`go test ./...`、`go vet ./...`

## Previous 0.3.34

本次发布完善 Antigravity 账号的 Gemini 额度窗口和窗口用量展示。

## Antigravity 额度

- 按 CPA 管理面板的实际契约调用 `retrieveUserQuotaSummary`，不再只显示 `loadCodeAssist` 返回的 AI Credits。
- 读取认证文件中的 `project_id`，并兼容 `projectId`、`cloudaicompanionProject` 等项目字段。
- 支持 Gemini 模型、Claude 和 GPT 模型等分组，以及 5 小时、日、周、月额度窗口。
- 保存额度百分比、刷新时间和分组说明；刷新失败时保留上一次成功数据。
- Gemini 额度按真实 5 小时和周窗口归因 usage_records，计算请求数、Tokens 和费用。

## 页面展示

- 账号表格直接显示 Antigravity 分组额度条。
- 进度条卡片和圆环卡片均显示 Antigravity 各额度窗口。
- Antigravity 页面只展示 Gemini 周限额和 5 小时限额，窗口用量与 GPT 使用相同的请求、Tokens、费用卡片。
- 旧 AI Credits 文本仅作为未取得分组额度时的兼容兜底。

## 数据迁移

- 新增迁移 `202607180003`，持久化 Antigravity 分组额度数据。
- 升级后刷新一次 Antigravity 账号即可显示额度条。

## 验证

- 后端：`go test ./...`、`go build ./cmd/cpa-helper`
- 前端：`npm.cmd run build`、`npm.cmd run lint`、`npm.cmd run test:i18n`
