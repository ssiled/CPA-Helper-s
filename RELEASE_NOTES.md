# CPA-Helper 0.3.26

本次发布修复号池权限撤销和请求热路径状态一致性问题，并配合 CLIProxyAPI 与 CPA Auth Pool Plugin 修复“号池内有正常账号但返回 503”的跨项目候选优先级问题。

## 权限与一致性

- 管理员从号池授权列表移除用户时，自动解除该用户已有 API Key 号池绑定。
- 解除绑定会同步到 CPA 插件；本地事务失败时尝试恢复远端绑定，降低双写不一致风险。
- Helper 代理请求缺少对应插件绑定时，由 Plugin `v0.1.24` fail-closed，不再回退到 CPA 全局账号。

## 请求热路径

- 模型请求和模型列表过滤改为读取 Helper DB 中的 last-good 号池模型快照。
- CPA Management `/status` 暂时不可用时，不再阻断正常模型请求。
- 禁用或不存在的本地号池继续按 fail-closed 处理。

## 配套版本

- CPA Auth Pool Plugin `v0.1.24` 或更高版本。
- 包含 auth-pool priority-filter 修复的 `ssiled/CLIProxyAPI` main 镜像。

## 验证

- 后端：`go test ./...`
- 前端：`npm run build`
