# CPA-Helper 0.3.33

本次发布补齐 Antigravity 账号的真实分组额度查询与额度条展示。

## Antigravity 额度

- 按 CPA 管理面板的实际契约调用 `retrieveUserQuotaSummary`，不再只显示 `loadCodeAssist` 返回的 AI Credits。
- 读取认证文件中的 `project_id`，并兼容 `projectId`、`cloudaicompanionProject` 等项目字段。
- 支持 Gemini 模型、Claude 和 GPT 模型等分组，以及 5 小时、日、周、月额度窗口。
- 保存额度百分比、刷新时间和分组说明；刷新失败时保留上一次成功数据。

## 页面展示

- 账号表格直接显示 Antigravity 分组额度条。
- 进度条卡片和圆环卡片均显示 Antigravity 各额度窗口。
- 旧 AI Credits 文本仅作为未取得分组额度时的兼容兜底。

## 数据迁移

- 新增迁移 `202607180003`，持久化 Antigravity 分组额度数据。
- 升级后刷新一次 Antigravity 账号即可显示额度条。

## 验证

- 后端：`go test ./...`、`go build ./cmd/cpa-helper`
- 前端：`npm.cmd run build`、`npm.cmd run lint`、`npm.cmd run test:i18n`
