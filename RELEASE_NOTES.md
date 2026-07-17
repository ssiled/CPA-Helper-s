# CPA-Helper 0.3.25

本次发布新增 CPA 号池插件监控日志，用于直接排查账号明明存在但调度返回 503、并发已满或上游请求失败等问题。

## 管理界面

- 新增“账号巡检 → 插件监控日志”页面，每 10 秒自动刷新。
- 展示调度阶段、状态、CPA 目标、号池、模型、最终账号、候选数、HTTP 状态、耗时与失败原因。
- 支持按目标、阶段、状态、号池筛选，以及按账号、模型、用户和原因搜索。
- 详情抽屉展示候选账号样本、优先级、账号状态和动态账号类型。
- 支持清空所有已配置 CPA 目标的内存事件日志，并单独显示无法访问的目标。

## 后端

- 新增管理员接口 `GET /api/auth-pools/plugin-events` 和 `DELETE /api/auth-pools/plugin-events`。
- 聚合所有启用且配置 Management Key 的 CPA 目标；单个目标失败不会阻断其他目标返回。
- 事件结构不采集 CPA API Key、Management Key、Authorization 请求头或请求正文。

## 兼容性

- 此页面需要 CPA Auth Pool Plugin `v0.1.23` 或更高版本；旧插件会对事件接口返回 404。
- 插件事件仅保存在 CPA 进程内存中，CPA 重启或手动清空后会消失。

## 验证

- 后端：`go test ./...`
- 前端：`npm run build`
