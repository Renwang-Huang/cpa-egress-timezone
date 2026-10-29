# CPA Egress Timezone

根据 CPA 选中账号的**可观测出口 IP**，将 Codex / OpenAI 请求中的客户端时区声明自适应为对应的 IANA 时区。

[English](README.en.md) · [配置示例](examples/plugin-config.yaml) · [原理与边界](docs/DESIGN.md) · [发布与商店](docs/PUBLISHING.md)

- 插件 ID：`egress-timezone`
- 版本：`2.0.0`
- 许可证：MIT
- 源码仓库：[Renwang-Huang/cpa-egress-timezone](https://github.com/Renwang-Huang/cpa-egress-timezone)
- 兼容基线：支持动态插件的 CPA `v8.0.47`，C ABI v1 / 插件协议 v2
- 发布平台：Linux amd64 / arm64，glibc；尚未提供 macOS、Windows 或 musl 构建

## 工作方式

1. 在 CPA 选择凭据之后读取 `selected_auth_id`，从本机 CPA 配置和对应 Codex 凭据文件还原该账号的代理和目标源站。
2. 使用该代理访问目标源站的 `/cdn-cgi/trace`，读取该源站返回的 `ip=`。也可以显式配置 JSON 探测地址。
3. 查询这个**已观测 IP**的 IANA 时区，重写受支持的时区声明；不是查询 GeoIP 服务自己看到的 IP。
4. 缓存到期后的下一次请求重新探测。出口变化时使用新的时区；探测失败时默认保留原始请求。

这是同一路由上的独立探测，**无法证明每一条模型连接使用了相同的出口 IP**。按连接轮换、按目标路径分流、自定义传输、SDK 请求级代理覆盖、额外网关转发，都可能使探测与模型请求不一致。自动模式适用于出口在缓存周期内稳定、探测与模型请求路由一致的部署，详见[边界说明](docs/DESIGN.md)。

## 修改范围

| 内容 | 行为 |
| --- | --- |
| `<environment_context>` 内 `<timezone>` | 改为出口 IP 对应时区 |
| 最近环境块内 `<current_date>` | 仅当它确实等于原时区的今天且声明不歧义时，换为目标时区的今天 |
| 明确的 `timezone` / `timeZone` / `client_timezone` 等字段 | 在支持的客户端上下文、元数据、Web 搜索位置字段中修改 |
| 已存在的明确时区请求头 | 在 after-auth 钩子中修改；不会凭空新增时区请求头 |
| 普通正文、工具参数、工具结果、加密 reasoning、助手历史 | 保留 |
| 旧环境块的日期、历史日期、来源时区未知的日期 | 保留 |
| User-Agent、Originator、Authorization、账号与路由配置 | 不由插件修改 |

使用 Go 内嵌 tzdata，按 IANA 时区处理夏令时与跨日，不依赖服务器的 `TZ`。本插件不修改主机/容器时区，也不改写普通文本中的所有地点或系统信息。

## 安装

### 从源码构建

需要 Go（`go.mod` 固定工具链为 `go1.27.3`）、C 编译器，以及支持 CGO 动态插件的 CPA。

```bash
bash scripts/build.sh
go test -race -count=1 ./...
go vet ./...
```

原生构建输出：`build/linux/<架构>/egress-timezone.so`。将文件复制到 CPA 插件目录，例如容器内 `plugins/linux/arm64/egress-timezone.so`，然后把[示例配置](examples/plugin-config.yaml)合并到现有配置中。

交叉编译 ARM64 时需要目标平台 C 编译器：

```bash
CC=aarch64-linux-gnu-gcc GOOS=linux GOARCH=arm64 bash scripts/build.sh
```

glibc 构建不能作为 Alpine / musl 构建使用。二进制的 glibc 要求也必须由运行环境满足。

### 从插件商店安装

可以使用 [GitHub Releases](https://github.com/Renwang-Huang/cpa-egress-timezone/releases) 的正式版本，或添加自定义商店源：

```yaml
plugins:
  enabled: true
  store-sources:
    - https://raw.githubusercontent.com/Renwang-Huang/cpa-egress-timezone/main/store/registry.json
```

随后在 CPA 管理页的「插件 → 商店」中安装 **Egress Timezone**。自定义源读取本仓库的最新正式 Release；官方商店收录仍需上游 registry PR 合并，详见[发布说明](docs/PUBLISHING.md)。

### 从旧插件迁移

旧 ID 为 `los-angeles-timezone`，新 ID 为 `egress-timezone`。先备份现有 CPA 配置和旧 `.so`，禁用旧插件，再启用新插件，避免两个插件先后覆盖结果。

```yaml
plugins:
  enabled: true
  configs:
    los-angeles-timezone:
      enabled: false
    egress-timezone:
      enabled: true
      priority: 1000
      mode: auto
      cpa_config_path: /CLIProxyAPI/config.yaml
```

插件需要读取 CPA 运行环境中的配置路径。OAuth Codex 还需要读取 `oauth.auth-dir`（旧配置为 `auth-dir`）下选中凭据的 JSON。只读取路由相关字段，不会将文件内容发送给探测端点。文件加密、动态账号、Home 下发的非文件凭据或权限不足时，自动模式会跳过改写。

原地覆盖已经加载的同一路径 `.so` 不保证热重载；通过商店升级后检查实际激活版本，必要时在维护窗口重启 CPA。回滚时禁用 `egress-timezone`，恢复旧插件及已备份配置。

## 配置

配置位于 `plugins.configs.egress-timezone`。CPA 管理界面也可以显示插件声明的这些字段。

| 配置 | 默认值 | 说明 |
| --- | --- | --- |
| `mode` | `auto` | `auto` 或 `fixed` |
| `cpa_config_path` | 自动发现 | `--config` / `-config` 参数，否则 `./config.yaml`；容器内路径 |
| `probe_url` | 空 | 空时探测上游源站 `/cdn-cgi/trace`；自定义端点必须与模型请求使用相同出口规则 |
| `geoip_url` | `https://ipwho.is/{ip}` | 必须且只能有一个 `{ip}` 占位符 |
| `cache_ttl_seconds` | `60` | 路由观测有效期，1–86400 秒；过期后不用旧观测改写 |
| `geoip_cache_ttl_seconds` | `21600` | IP → 时区缓存，1–604800 秒；不影响出口重探测频率 |
| `retry_seconds` | `10` | 失败重试冷却，1–3600 秒 |
| `lookup_timeout_ms` | `4000` | 一次出口探测和必要 GeoIP 查询的总预算，100–15000 毫秒 |
| `fallback_timezone` | 空 | 失败时保持原请求；非空则显式回退到指定 IANA 时区 |
| `fixed_timezone` | `America/Los_Angeles` | 仅 `fixed` 模式使用；该模式不访问探测与 GeoIP 服务 |
| `address_family` | `auto` | `auto` / `ipv4` / `ipv6`；强制族只适用于直接连接，显式代理必须为 `auto` |
| `max_cached_routes` | `128` | 路由缓存和 GeoIP 缓存各自的容量上限，1–4096 |
| `allow_http` | `false` | 允许明文探测/源站；仅用于受控本地测试 |

探测端点支持 Cloudflare trace 的 `ip=...`，或 `{"ip":"公共 IP"}`。GeoIP 端点需要返回相同 `ip`，以及 `timezone.id` 或字符串 `timezone`。不会根据国家缩写猜时区。

默认 GeoIP 服务需要互联网连接，有配额和可用性限制。公共端点的政策以[服务商文档](https://ipwhois.io/documentation)为准；可配置自己的兼容服务。插件将已观测公共 IP 提交给 GeoIP 服务，不携带账号令牌、请求正文或客户端身份请求头。管理状态含观测 IP，应通过 CPA 管理鉴权保护。

## 验证与诊断

```bash
go test -race -count=1 ./...
go vet ./...
bash scripts/build.sh
python3 scripts/integration.py
python3 scripts/package.py --arch amd64
```

集成测试启动独立 CPA 容器、本地假上游和两条可控代理，使用假密钥。测试普通/流式 Responses、Chat Completions、WebSocket、多账号出口隔离、IP 变化、缓存、失败保留原请求，以及身份请求头与禁用插件时一致。它不会调用真实模型 API；合成 IP / GeoIP 结果不能视为生产出口验证。结果写入忽略目录 `build/`。

如需显式进行真实 trace/GeoIP 补测，在与 CPA 相同的网络/文件系统环境中运行：

```bash
CPA_EGRESS_LIVE_CONFIG=/CLIProxyAPI/config.yaml \
  go test -run '^TestLiveProbeOptIn$' -v
```

可以另设 `CPA_EGRESS_LIVE_AUTH_ID` 指定凭据；否则选择第一个可读、未禁用的 Codex JSON 文件。该测试不调用模型 API。正常 CI 不设置这些变量，不会访问真实账号。当前验证结果与首次网络超时记录见 [VERIFICATION.md](docs/VERIFICATION.md)。

插件状态接口（自定义插件路由使用 `v0` 前缀）：

```bash
curl --fail --silent --show-error \
  -H "Authorization: Bearer $CPA_MANAGEMENT_KEY" \
  http://127.0.0.1:8317/v0/management/plugins/egress-timezone/status
```

查看 `observations[].ip/timezone/observed_at/expires_at/last_error`、`last_skip_reason` 和计数器。失败后的旧观测只用于诊断展示，不代表仍用于改写。缓存只在内存中，重载配置后清空；插件不持久保存账号或探测记录。

## 目录

```text
main.go                 C ABI 入口与内存释放
plugin.go               生命周期、after-auth 钩子、管理接口
route.go                选中凭据的路由解析，兼容 legacy/v8 配置
resolver.go             出口探测、GeoIP、并发合并、容量及超时控制
normalize.go            选择性环境时区和日期改写
*_test.go               Go 行为及竞态测试
examples/               可合并的配置示例
scripts/                构建、隔离集成测试、商店打包
store/registry.json     自定义源及官方收录 PR 的条目
.github/workflows/      CI 与草稿 Release 构建
docs/                   原理、验证、发布、Release 说明
```

历史私有部署资料、账号、密码、生产日志和原 CPA 全量源码不属于本项目发布内容。
