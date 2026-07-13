# CPA-Helper 0.3.22

本次发布主要修复 CPA-Helper-s 在卡网收录与 Docker 部署流程中的问题。

## 修复

- 修复卡网收录价格不显示：当上游 `productItems` 为空时，会从 `productSummary.groups[].previewItems[]` 归一化商品信息，恢复价格、库存、销量与分组显示。
- 为卡网收录上游请求补充 `User-Agent`，避免部分环境被源站拒绝请求。

## 部署

- `docker-compose.yml` 改为使用 `ghcr.io/ssiled/cpa-helper-s:latest`，服务器无需本地高负载构建。
- 新增独立 GHCR 镜像发布工作流，推送 `main` 后自动构建并发布 `latest` 镜像。

## 验证

- 已新增卡网价格结构漂移回归测试。
- 已通过后端全量测试：`go test ./...`。
