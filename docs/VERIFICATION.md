# 2.0.0 本地验证记录

日期：2026-10-29。此记录区分隔离测试、真实探测和未完成的公开发布。

## 构建与代码检查

| 项目 | 结果 |
| --- | --- |
| Linux amd64 原生构建 | 通过，Go 1.27.3，CGO c-shared，glibc |
| Linux arm64 原生构建 | 在 ARM64 Ubuntu 主机通过，Go 1.27.3，CGO c-shared，glibc |
| `go test -race -count=1 ./...` | 两种架构均通过 |
| `go vet ./...` | amd64 通过 |
| Python 脚本语法、构建脚本语法 | 通过 |
| GitHub Actions YAML | 本地解析通过；尚未在 GitHub 执行 |

Go 测试覆盖：选择性环境/日期处理、夏令时和跨年、普通正文与不透明对象保持、
数字精度、legacy/v8 配置路由、继承与覆盖、缓存隔离、并发查询合并、出口变化、
过期观测拒绝、冷却、超时、容量、响应体上限、重定向/无效 IP/无效 IANA 拒绝、
失败保留原请求、显式固定回退，以及身份头策略。

## CPA 隔离集成

镜像：`eceasy/cli-proxy-api:v8.0.47`，程序 commit `90c447d`。
amd64 与 arm64 **各通过 12 项**：

1. 禁用插件时的基线。
2. Responses 非流式。
3. Responses 流式。
4. 账号 B 使用独立代理与时区。
5. Chat Completions。
6. 出口不变时复用 GeoIP 映射。
7. 出口变化后刷新时区。
8. 刷新失败时保留来源时区。
9. 已鉴权管理状态接口。
10. 探测不携带 provider identity。
11. CPA v8 分组配置及代理 B。
12. Responses WebSocket。

每条模型请求在本地假上游捕获实际传输结果，同时核对 User-Agent / Originator
与禁用插件时一致。IP 和 GeoIP 回复为合成 fixture，不是公网真实出口证明。
没有调用真实模型 API。

## 实际网络探测

在现有 CPA 容器的网络命名空间中，用新的临时测试容器运行 `TestLiveProbeOptIn`。
配置与账号目录只读挂载，未加载到生产 CPA，也没有请求真实模型端点。

- 容器内确认普通 Codex JSON 凭据可读取；宿主普通用户读取其物理文件时的权限
  错误，没有被误判成凭据加密。
- 首次默认 4 秒预算：trace 已返回公共 IP，GeoIP 查询超时，测试失败。
- 独立可达性检查：trace 约 1.25 秒；默认 GeoIP 查询约 1.29 秒且 IP 匹配，
  返回 `America/Los_Angeles`。
- 随后重跑同一 Go 探测、同一默认 4 秒预算：通过，总耗时约 3.23 秒，源站
  trace 公共 IP → `America/Los_Angeles`。IP 与账号标识没有写入公开验证文档。

这两次结果同时保留在本地忽略目录 `build/live-probe.log`。未通过增加预算或
替换返回结果掩盖首次失败。外部网络延迟存在波动；生产使用时可按自己的延迟预算
配置 `lookup_timeout_ms`，探测失败时仍默认保留原请求。

真实探测只验证了该探测请求。没有证明所有模型 HTTP/WS 连接的逐请求出口，
也没有验证真实账号的模型生成或推理成功率。

## 发布包与目录

- 两种架构 zip 的 ELF 机器类型正确；zip 根目录只有一个目标动态库。
- zip 完整性与 SHA-256 校验通过；包含项目与依赖许可证。
- `store/registry.json` 必需字段及 ID 格式通过本地检查。
- 公开源码目录扫描未发现已知私有部署地址、服务器路径、私钥或常见令牌模式；
  集成测试仅含明显的假密钥。
- 旧项目及部署资料保存在本机项目目录之外，不属于该 Git 仓库。
- 测试临时目录和容器已清理。没有修改生产配置、替换生产插件或重启生产容器。

本地证据在忽略目录：`build/integration-results.json`（amd64）、
`build/integration-arm64.json`、`build/live-probe.log`。

## 本地验证结束时的发布边界

上述本地验证完成时，尚未创建/推送公开 GitHub 仓库、运行 GitHub Actions、发布
正式 GitHub Release 或提交官方商店 PR；这些本地测试不包括公开商店安装。
后续公开 CI、正式 Release 与商店 PR 的状态应分别以仓库 Actions、Releases 和
上游 PR 页面为准。具体操作见 [PUBLISHING.md](PUBLISHING.md)。
