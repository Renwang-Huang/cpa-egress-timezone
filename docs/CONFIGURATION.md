# 配置参考

[← 项目首页](../README.md) · [English](CONFIGURATION.en.md) · [完整示例](../examples/plugin-config.yaml)

## 配置位置

配置位于 `plugins.configs.egress-timezone`。CPA 管理界面也可以显示插件声明的这些字段。请合并到现有配置，不要用示例替换整个配置文件。

```yaml
plugins:
  enabled: true
  configs:
    egress-timezone:
      enabled: true
      priority: 1000
      mode: auto
      cpa_config_path: /CLIProxyAPI/config.yaml
```

`enabled` 与 `priority` 为 CPA 插件加载设置。以下是本插件的配置项。

## 所有选项

| 配置 | 默认值 | 说明 |
| :--- | :--- | :--- |
| `mode` | `auto` | `auto` 或 `fixed` |
| `cpa_config_path` | 自动发现 | 从 `--config` / `-config` 参数发现，否则 `./config.yaml`；使用容器内路径 |
| `probe_url` | 空 | 空时探测上游源站 `/cdn-cgi/trace`；自定义端点必须与模型请求使用相同出口规则 |
| `geoip_url` | `https://ipwho.is/{ip}` | 必须且只能有一个 `{ip}` 占位符 |
| `cache_ttl_seconds` | `60` | 路由观测有效期，1–86400 秒；过期后不用旧观测改写 |
| `geoip_cache_ttl_seconds` | `21600` | IP → 时区缓存，1–604800 秒；不影响出口重探测频率 |
| `retry_seconds` | `10` | 失败重试冷却，1–3600 秒 |
| `lookup_timeout_ms` | `4000` | 一次出口探测和必要 GeoIP 查询的总预算，100–15000 毫秒 |
| `fallback_timezone` | 空 | 失败时保持原请求；非空则显式回退到指定 IANA 时区 |
| `fixed_timezone` | `America/Los_Angeles` | 仅 `fixed` 模式使用，不访问探测与 GeoIP 服务 |
| `address_family` | `auto` | `auto` / `ipv4` / `ipv6`；强制族只适用于直接连接，显式代理必须为 `auto` |
| `max_cached_routes` | `128` | 路由缓存和 GeoIP 缓存各自的容量上限，1–4096 |
| `allow_http` | `false` | 允许明文探测/源站；仅用于受控本地测试 |

## 固定时区模式

不需要自动探测时，使用固定模式：

```yaml
plugins:
  enabled: true
  configs:
    egress-timezone:
      enabled: true
      priority: 1000
      mode: fixed
      fixed_timezone: America/Los_Angeles
```

固定模式不访问探测与 GeoIP 服务。内嵌 tzdata 按 IANA 规则处理夏令时。

## 探测与 GeoIP 端点

探测端点支持 Cloudflare trace 的 `ip=...`，或 `{"ip":"公共 IP"}`。GeoIP 端点需要返回相同 `ip`，以及 `timezone.id` 或字符串 `timezone`。插件不会根据国家缩写猜时区。

默认 GeoIP 服务需要互联网连接，有配额和可用性限制。政策以[服务商文档](https://ipwhois.io/documentation)为准；可配置自己的兼容服务。插件将已观测公共 IP 提交给 GeoIP 服务，不携带账号令牌、请求正文、cookie 或客户端身份请求头。

自定义源站没有 trace 接口时，需要配置与模型请求路由等价的 `probe_url`；否则自动模式默认保留原请求。探测与模型请求的准确性边界见[设计说明](DESIGN.md)。

## 文件读取要求

插件需要读取 CPA 运行环境中的配置路径。OAuth Codex 还需要读取 `oauth.auth-dir`（旧配置为 `auth-dir`）下选中凭据的 JSON。只读取路由相关字段，不会将文件内容发送给探测端点。

文件加密、动态账号、Home 下发的非文件凭据或权限不足时，自动模式会跳过改写。不要把无法读取的凭据误当成全局路由。

## 状态与诊断

自定义插件路由使用 `v0` 前缀；接口需要 CPA 管理鉴权：

```bash
curl --fail --silent --show-error \
  -H "Authorization: Bearer $CPA_MANAGEMENT_KEY" \
  http://127.0.0.1:8317/v0/management/plugins/egress-timezone/status
```

| 字段 | 含义 |
| :--- | :--- |
| `observations[].ip` / `timezone` | 已观测 IP 及其解析时区 |
| `observations[].observed_at` / `expires_at` | 观测时间与有效期 |
| `observations[].last_error` | 脱敏后的探测或查询错误 |
| `last_skip_reason` | 最近跳过改写的原因 |
| 计数器 | 请求处理、改写与查询情况 |

失败后的旧观测只用于诊断展示，不代表仍用于改写。缓存只在内存中，重载配置后清空；插件不持久保存账号或探测记录。状态含观测 IP，应通过 CPA 管理鉴权保护。

## 常见问题

### 为什么第一次请求或缓存到期后更慢？

首次请求或过期后的下一次请求同步探测，默认总查询预算为 4 秒。同一路由的并发查询会合并。缓存期间不重复探测；没有后台轮询。可按自己的延迟预算调整 `lookup_timeout_ms`。

### 为什么出口变了，时区没有立即变化？

路由观测默认有效 60 秒，只有到期后的下一次请求才刷新。GeoIP 映射独立缓存 6 小时；新 IP 会使用对应映射，不需要等待旧 IP 的 GeoIP 缓存过期。

### 为什么请求没有改写？

查看 `last_skip_reason` 与 `last_error`，确认插件实际激活、配置与凭据可读、选中账号受支持，以及 trace / GeoIP 可达。过期后刷新失败，默认保留原请求。仅在明确需要时设置 `fallback_timezone`。

### 可以强制 IPv4 或 IPv6 吗？

`address_family` 只约束直接连接的拨号。显式代理配合非 `auto` 地址族会跳过，无法用此选项改变代理或 CPA 模型连接的地址选择。
