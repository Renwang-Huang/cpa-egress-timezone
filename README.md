<picture>
  <source media="(prefers-color-scheme: dark)" srcset="assets/hero-dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="assets/hero-light.svg">
  <img alt="CPA Egress Timezone — A timezone that follows your route." src="assets/hero-light.svg" width="100%">
</picture>

<div align="center">

# 让时区，跟随出口。

根据 CPA 选中账号的**可观测出口 IP**，自动适配 Codex / OpenAI 请求中的时区声明。

[![Release](https://img.shields.io/github/v/release/Renwang-Huang/cpa-egress-timezone?style=flat-square&color=0e8a78&label=release)](https://github.com/Renwang-Huang/cpa-egress-timezone/releases/latest) [![Verify](https://img.shields.io/github/actions/workflow/status/Renwang-Huang/cpa-egress-timezone/ci.yml?branch=main&style=flat-square&label=verify)](https://github.com/Renwang-Huang/cpa-egress-timezone/actions/workflows/ci.yml) [![Platform](https://img.shields.io/badge/Linux-amd64%20%7C%20arm64-52637b?style=flat-square)](docs/INSTALLATION.md#兼容性) [![License: MIT](https://img.shields.io/badge/license-MIT-52637b?style=flat-square)](LICENSE)

**简体中文** · [English](README.en.md)

[快速开始](#快速开始) · [配置参考](docs/CONFIGURATION.md) · [原理边界](docs/DESIGN.md) · [下载](https://github.com/Renwang-Huang/cpa-egress-timezone/releases/latest)

</div>

---

## 多个账号，多个出口，各自的时区。

CPA 为不同账号选择不同代理时，客户端声明的时区可能与出口所在时区不一致。Egress Timezone 在账号选定后解析该路由，探测源站看到的公共 IP，再将受支持的环境声明调整为对应的 IANA 时区。

- **跟随账号路由**：解析选中凭据的代理与源站，分别缓存路由观测。
- **按需刷新观测**：默认 60 秒有效，到期后的下一次请求重新探测。
- **保留原始语义**：选择性修改时区，查询失败时默认保留原请求。

## 快速开始

需要支持动态插件的 **CPA v8.0.47**（兼容基线）及 **Linux amd64 / arm64 · glibc** 环境。[其他安装方式 →](docs/INSTALLATION.md)

### 1. 添加商店源

将以下字段合并到现有 CPA 配置，保留已有商店源及其他配置：

```yaml
plugins:
  enabled: true
  store-sources:
    - https://raw.githubusercontent.com/Renwang-Huang/cpa-egress-timezone/main/store/registry.json
```

### 2. 安装并启用

在 CPA 管理页打开 **插件 → 商店**，刷新后安装 **Egress Timezone**。将以下配置合并到同一个 `plugins` 节点：

```yaml
plugins:
  configs:
    egress-timezone:
      enabled: true
      priority: 1000
      mode: auto
      cpa_config_path: /CLIProxyAPI/config.yaml
```

`cpa_config_path` 使用 **CPA 运行环境内**的实际路径。插件需要读取配置；文件型 OAuth 账号还需要读取选中的凭据 JSON。旧版 `los-angeles-timezone` 用户请先按[迁移说明](docs/INSTALLATION.md#从旧插件迁移)禁用旧插件。

### 3. 查看观测结果

发送一条正常请求后，查询已鉴权的状态接口：

```bash
curl --fail --silent --show-error \
  -H "Authorization: Bearer $CPA_MANAGEMENT_KEY" \
  http://127.0.0.1:8317/v0/management/plugins/egress-timezone/status
```

重点查看 `observations` 中的 `ip`、`timezone`、`expires_at` 和 `last_error`。实际部署地址、管理密钥按自己的环境设置。[配置与故障排查 →](docs/CONFIGURATION.md)

## 改写效果

假设选中账号的源站探测 IP 对应 `America/Los_Angeles`，请求中的环境块会做如下调整：

```diff
 <environment_context>
-  <timezone>Asia/Hong_Kong</timezone>
+  <timezone>America/Los_Angeles</timezone>
 </environment_context>
```

| 内容 | 处理方式 |
| :--- | :--- |
| 环境块中的 `<timezone>`、支持的明确时区字段 | 适配为探测 IP 对应的 IANA 时区 |
| 最近环境块的 `<current_date>` | 仅当日期等于原时区的今天、且来源时区不歧义时转换 |
| 已存在的明确时区请求头 | 在 after-auth 钩子中调整，不新增时区请求头 |
| 普通正文、工具参数与结果、加密 reasoning、助手历史、历史日期 | 保留 |
| `User-Agent`、`Originator`、`Authorization`、账号及路由配置 | 保留 |

内嵌 IANA tzdata 处理夏令时与跨日；无需修改主机或容器时区。

## 工作方式与适用范围

1. **选择路由** — 在 after-auth 钩子中读取 `selected_auth_id`，解析选中账号的代理和源站。
2. **观测出口** — 经该路由请求源站 `/cdn-cgi/trace`，读取源站返回的公共 IP。
3. **解析时区** — 用这个已观测 IP 查询 GeoIP，缓存 IP → IANA 时区映射。
4. **适配请求** — 选择性改写受支持的声明；过期后重新探测，失败默认保留原请求。

> [!IMPORTANT]
> 探测使用独立连接，不能证明每一条模型连接的实际出口。自动模式适用于探测与模型请求路由一致、出口在缓存周期内稳定的部署。连接级轮换、按目标分流、请求级代理覆盖或额外转发网关可能造成差异。详见[原理与准确性边界](docs/DESIGN.md)。

默认 GeoIP 服务只接收已观测的公共 IP，不携带模型正文、账号令牌或客户端身份请求头；也可配置自己的兼容服务。[完整配置 →](docs/CONFIGURATION.md)

## 文档与开发

| 入口 | 内容 |
| :--- | :--- |
| [安装与迁移](docs/INSTALLATION.md) | 发行包、源码构建、平台要求、旧插件迁移与回滚 |
| [配置参考](docs/CONFIGURATION.md) | 所有选项、固定模式、诊断接口与常见问题 |
| [配置示例](examples/plugin-config.yaml) | 可合并到现有 CPA 配置的完整示例 |
| [设计说明](docs/DESIGN.md) | 路由解析、缓存策略、改写范围与准确性边界 |
| [验证记录](docs/VERIFICATION.md) | 双架构构建、隔离集成、真实探测与已知限制 |
| [发布与商店](docs/PUBLISHING.md) | Release 工作流、自定义源与官方商店收录流程 |

```bash
go test -race -count=1 ./...
go vet ./...
bash scripts/build.sh
python3 scripts/integration.py
```

集成测试使用独立 CPA 容器、假上游与假密钥，覆盖 HTTP、流式响应、WebSocket、多账号出口和失败处理，不调用真实模型 API。合成测试结果不代表生产出口验证；真实探测方法见[验证指南](docs/INSTALLATION.md#开发验证)。

---

<div align="center">

[MIT](LICENSE) · Created by [Renwang Huang](https://github.com/Renwang-Huang) · [第三方声明](THIRD_PARTY_NOTICES.md)

</div>
