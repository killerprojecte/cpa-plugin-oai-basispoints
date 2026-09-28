# 本项目（oai-basispoints）与真实 BPS 协议的差异与对齐

基线：`docs/basispoints-protocol.md`（来自扩展 HTML/JS，HAR 交叉验证）。
对象：本仓库 `internal/basispoints/*`。

> **范围声明：本项目的 `run_officejs` 中继“转译”层是刻意设计，不属于差异，保持不动。**
> 本文只对齐“与真实 BPS 服务通讯的外层协议”（URL/头/请求体/时序）。

---

## 0. 结论摘要

本项目定位是 **headless 的 `/responses` 代理**：它只复刻了真实插件与 BPS 交互中“模型推理”这一条链路，未实现 UI 类端点（标题/建议/技能/连接器）与 ARC 远程控制链路。这本身不是缺陷——只要 `/responses` 可用即可。

真正的**协议保真度问题**集中在三点：

1. **客户端画像头不是“推导”而是“硬编码默认可调”**：默认值与真实抓包（Windows/Edge）不符（默认 macOS/Chrome）；`Browser-Name` 被写死 `chrome`。
2. **`bps_tools_version_id` 默认为空**：真实客户端**总是**发送对应 editor 的 tools version id（excel = `tools-excel-core-2026-06-16-3af59f22`），本项目只有显式配置 `tools_version_id` 才发送。
3. **`context_management` / `reasoning_effort` 的默认与取值集合与真实不一致**：真实客户端总是发送由模型目录推导的 compaction 策略，且支持 `none`；本项目对 `none` 会静默改成 `medium`。

其余（Referer 结构、`_host_Info`、私有头集合、`/responses` body 字段名、WS 传输、attachments、auth 头）**已高度对齐**，仅有个别可选头与浏览器自动头缺失。

---

## 1. 已对齐部分（无需改动）

| 项 | 真实协议 | 本项目 | 结论 |
|---|---|---|---|
| base URL | `https://bps.openai.com/basispoints/api` | `responses_url` 默认同值 | ✅ |
| `Authorization` | `Bearer <access_token>` | 同（[upstream.go](internal/basispoints/upstream.go#L97)） | ✅ |
| `chatgpt-account-id` / `x-openai-account-id` | 均发送 | 均发送（[upstream.go](internal/basispoints/upstream.go#L98-L99)） | ✅ |
| `x-openai-account-user-id` | 复合用户 id | 同，取不到则跳过（[upstream.go](internal/basispoints/upstream.go#L119-L121)） | ✅ |
| `x-basispoints-auth-mode` | `chatgpt` | 同（[upstream.go](internal/basispoints/upstream.go#L100)） | ✅ |
| 私有头像集合 | `X-Openai-Internal-Basispoints-Client-* / Office-* / Browser-*` | 名称、大小写形式一致（[upstream.go](internal/basispoints/upstream.go#L60-L84)） | ✅ |
| `Origin` | `https://bps.openai.com` | 同 | ✅ |
| `Referer` 结构 | `…/extension/<pid>/?et=…&_host_Info=Excel$Win32$16.01$<locale>$$$$16` | 同结构（[upstream.go](internal/basispoints/upstream.go#L113)） | ✅（locale 见 §2.6） |
| `/responses` body 字段 | `model/input/stream/store/context_management/metadata/reasoning_effort/model_selection` | 同名字段（[protocol.go](internal/basispoints/protocol.go#L583-L626)） | ✅ |
| `metadata` 键名 | `task_id/turn_id/bps_tools_version_id/agent_iteration` | 同名（[protocol.go](internal/basispoints/protocol.go#L620-L625)） | ✅ |
| `agent_iteration` 类型 | 字符串 | 字符串 | ✅ |
| `model_selection` | `"explicit"` | `"explicit"`（[protocol.go](internal/basispoints/protocol.go#L585)） | ✅ |
| `store` | `false` | `false` | ✅ |
| `/attachments` 上传 | `POST {base}/attachments` multipart `file` → `{openai_file_id}` | 同（[attachments.go](internal/basispoints/attachments.go#L120-L226)） | ✅ |
| WS URL 查询参数 | `bps_client_info/bps_auth_mode/bps_ws_affinity/bps_control_frames` | 同（[transport_ws.go](internal/basispoints/transport_ws.go#L37-L61)） | ✅ |
| WS 子协议 | `["responses","openai-bearer.<token>"]` | 同（[transport_ws.go](internal/basispoints/transport_ws.go#L119-L127)） | ✅ |
| WS 首帧 | `{…body, type:"response.create", basispoints_request_id, basispoints_resume_token}` | 同（[transport_ws.go](internal/basispoints/transport_ws.go#L214-L221)） | ✅ |
| 终态/失败事件集合 | `response.completed/incomplete/failed/cancelled/error`、`server_draining` | 已处理（[response.go](internal/basispoints/response.go#L82-L97)、[transport_ws.go](internal/basispoints/transport_ws.go#L317-L332)） | ✅ |
| `previous_response_id` / `service_tier` | 真实客户端不发 | 本项目显式拒绝并给出明确错误（[protocol.go](internal/basispoints/protocol.go#L556-L562)） | ✅ |

---

## 2. 差异清单

### 2.1 【高】`bps_tools_version_id` 默认为空

- **真实**：`metadata.bps_tools_version_id` 总是存在，取当前 editor 的工具版本 id：
  `excel → tools-excel-core-2026-06-16-3af59f22`（源码 `wr`/`F_` 常量表）。
- **本项目**：仅当 `tools_version_id` 配置非空才写入（[protocol.go](internal/basispoints/protocol.go#L623-L625)）；默认配置为空 → **不发送**。
- **影响**：上游可能按缺省（最新/默认）工具版本处理，与客户端实际工具集不匹配；`/responses` 响应头 `x-openai-internal-basispoints-tools-version-id` 会回显一个与客户端不一致的版本。
- **对齐**：默认按 editor 填 `tools-excel-core-2026-06-16-3af59f22`（或在未配置时回退该值）。建议把它设为 `defaultConfig()` 的默认值，而不是依赖用户填写。

### 2.2 【高】客户端画像头：默认值与推导逻辑不符

- **真实**：`Browser-Name`、`UA-Platform`、`UA-Brands`、`User-Agent` 全部由 `navigator` 运行时推导；Excel 桌面实测 `edge` / `Windows` / `Microsoft Edge WebView2,Not_A Brand,Chromium,Microsoft Edge` / Edge UA。
- **本项目**：
  - `Browser-Name` 写死 `"chrome"`（[upstream.go](internal/basispoints/upstream.go#L79)），无法反映真实浏览器；
  - 默认 `UserAgent`/`UAPlatform`/`UABrands` 为 macOS Chrome（[types.go](internal/basispoints/types.go#L26-L29)），与抓包（Windows Edge）不符。
- **影响**：画像自相矛盾（UA 是 Edge 但 Browser-Name 是 chrome；或 UA 是 macOS 但 Office-Platform 是 PC）。BPS 可能据此做客户端指纹/风控。
- **对齐**：让默认值自洽——`User-Agent`、`UAPlatform`、`UABrands`、`Browser-Name` 作为一组默认（Windows + Edge 或 macOS + Chrome），并允许从 UA 推导 Browser-Name（`edg/`→edge、`chrome/`→chrome…）而不是写死。

### 2.3 【中】`context_management` 缺省行为

- **真实**：`enableResponsesCompact` 为真时**总是**发送 `[{type:"compaction", compact_threshold:<由模型目录推导>}]`（实测 200000）。
- **本项目**：仅当客户端请求里带 `context_management` 才透传（[protocol.go](internal/basispoints/protocol.go#L592-L596)）；未带则不发送。
- **影响**：长会话在真实客户端会自动压缩，本项目可能不压缩，导致上下文超限或与服务端预期不一致。
- **对齐**：可在未提供时按模型目录/内存上限补一个默认 compaction 策略（阈值需可配置）。

### 2.4 【中】`reasoning_effort` 取值集合与默认

- **真实**：集合 `["none","low","medium","high","xhigh"]`（`$se`）；缺省在 loop 内为 `"none"`，实际由模型目录 `default_effort`（实测 `medium`）决定；支持 `none`。
- **本项目**：`supportedReasoningEfforts = {low,medium,high,xhigh,ultra}`（[types.go](internal/basispoints/types.go#L41-L43)），`normalizeEffort` 把 `none` 归为 `medium`、把 `max` 归为 `xhigh`（[types.go](internal/basispoints/types.go#L357-L368)），并额外保留 `ultra`。
- **影响**：
  - 客户端请求 `none` 会被**静默改成** `medium`（语义被改）；
  - `ultra` 会原样透传，而真实协议无此值（可能被上游拒绝）。
- **对齐**：`none` 应原样保留（加入 allowlist）；`ultra` 需要确认上游是否接受，否则应映射/拒绝。

### 2.5 【中】`/responses` 之外的真实端点未实现

- **真实**：`register/bootstrap/ready/heartbeat`（ARC 生命周期）、ARC Client Sync WS、`/responses/access`（模型目录）、`/accounts/check`、`/connectors/apps`、`/arc/settings`、`/skills`、`/task-title`、`/prompt-suggestions`。
- **本项目**：只实现 `/responses`、`/attachments`，其余未实现（`/responses/compact` 显式拒绝）。
- **影响**：
  - 纯推理不受影响（`/responses` 可独立调用）；
  - 但**模型目录是静态配置**（`models`/`model_mappings`），不会随 `/responses/access` 动态变化；
  - 不参与 ARC，故不支持“Codex 远程控制工作簿”。
- **对齐（按需）**：
  - 若需要服务端权威模型目录/套餐限流，接入 `GET /responses/access?include_models=true`；
  - 若需远程控制，按 §5.2 实现 ARC 生命周期 + Client Sync WS。

> **待验证**：抓包中 `/responses` 均发生在已注册会话之后，无法据此判断 `/responses` 是否强依赖注册。本项目当前直接调用 `/responses`，生产可用，故推断注册非必需；若上游后续收紧，需补 ARC 注册。

### 2.6 【低】Referer 的 locale 与 `et` 令牌

- **真实**：`_host_Info=Excel$Win32$16.01$<locale>$$$$16`，locale 随客户端语言（实测 `zh-CN`）；`et` 是随会话下发、**会过期**的权益令牌。
- **本项目**：硬编码 `…$en-US$$$$16` 与一个**固定不变**的 `et` 令牌（[upstream.go](internal/basispoints/upstream.go#L113)）。
- **影响**：一般无（Referer 通常不被校验内容），但“完全一致”上不满足；硬编码过期令牌语义上不妥。
- **对齐**：把 `_host_Info` 的 locale 做成配置项（默认与 `Accept-Language` 一致），`pid`/`et` 可配置；或在无谓时省略 `et`（仅保留 path）。

### 2.7 【低】浏览器自动头的缺失

- **真实**（浏览器自动附加）：`sec-ch-ua`、`sec-ch-ua-mobile`、`sec-ch-ua-platform`、`sec-fetch-site/mode/dest`、`priority`、`baggage`、`sentry-trace`、`cookie`(cf_clearance/__cf_bm)、`accept-encoding: gzip, deflate, br, zstd`、`accept-language`。
- **本项目**：均不发送；且 `Accept-Encoding: identity`（[upstream.go](internal/basispoints/upstream.go#L103)）。
- **影响**：服务端若按 `sec-fetch-*`/`cf_clearance` 做反爬校验可能拒绝；`identity` 会禁用压缩（增加带宽，但简化解析）。
- **对齐**：非必需。若遇到 Cloudflare 拦截，需要补 `cf_clearance`（与 UA/TLS 绑定，需真实浏览器指纹）与 `sec-fetch-*`。

### 2.8 【低】`x-stainless-*` 的适用范围

- **真实**：仅 `/responses`（OpenAI JS SDK 6.31.0）带 `x-stainless-*`；`register`/`task-title`/`access` 等**不带**。
- **本项目**：`authHeaders` 对**所有**请求都注入 `x-stainless-*`（[upstream.go](internal/basispoints/upstream.go#L105-L111)）。
- **影响**：极小；仅 `/attachments` 等端点会带上不属于它的 SDK 头。
- **对齐**：把 `x-stainless-*` 限定在 `/responses`（可选）。

### 2.9 【低】缺少 `X-Openai-Internal-Basispoints-Office-Host-Version`

- **真实**：有 `Office.context.diagnostics.hostVersion` 时才发（本次抓包未发）。
- **本项目**：从不发。
- **影响**：无（可选头，抓包也未出现）。
- **对齐**：无需改。

---

## 3. 转译层（保持不动）

以下几处是**刻意的协议转译**，与真实插件协议不同，但**按要求不改**：

1. **工具中继**：真实客户端把 `tools` 定义直接放进 `/responses` body，模型产出真实原生工具调用（如 `run_officejs`）由客户端执行；本项目把客户端工具目录改写成 `run_officejs` 中继信封（`code` 内嵌 JSON），并在响应侧把中继调用还原成客户端工具调用。见 [protocol.go](internal/basispoints/protocol.go#L159-L198)、[protocol.go](internal/basispoints/protocol.go#L891-L940)。
2. **`instructions` 处理**：真实直接发 `instructions` 字段；本项目把它降级为 `input` 中的 developer message（[protocol.go](internal/basispoints/protocol.go#L574-L577)）。
3. **`additional_tools` 剥离**：客户端放在 `input` 里的工具目录不转发上游（[protocol.go](internal/basispoints/protocol.go#L429-L435)）。
4. **SSE 先缓冲后回放**：为保证工具调用可整项转换，先读完上游 SSE 再合成客户端 SSE（[protocol.go](internal/basispoints/protocol.go#L948-L1029)）。

---

## 4. 建议的对齐改动（不含转译层）

按性价比排序：

1. **默认 `tools_version_id`**：在 `defaultConfig()` 中默认 `tools-excel-core-2026-06-16-3af59f22`（见 2.1）。
2. **画像头自洽**：统一默认 UA/平台/品牌为一组，并让 `Browser-Name` 支持从 UA 推导，而非写死 `chrome`（见 2.2）。
3. **`reasoning_effort` 集合**：加入 `none` 并原样传递；确认 `ultra` 是否为上游合法值，否则移除或映射（见 2.4）。
4. **`context_management` 缺省**：未提供且需要时补默认 compaction 策略（阈值可配）（见 2.3）。
5. **`_host_Info` locale 与 `et`**：改为可配置，默认与 `Accept-Language` 对齐；`et` 不再硬编码固定令牌（见 2.6）。
6. （可选）**`x-stainless-*` 限定 `/responses`**（见 2.8）。
7. （可选）**接入 `/responses/access`** 以使用服务端权威模型目录（见 2.5）。

> 以上 1–6 均为**低风险、局部**改动，均在 `upstream.go` / `types.go` / `protocol.go` 内，不触碰转译层。

---

## 5. 本轮实施状态（对齐落地）

本节记录实际落地的改动；未列出的项表示**刻意不改**（见 §6）。

### 5.1 已落地

| 差异项 | 落地内容 | 配置项 |
|---|---|---|
| §2.2 `Browser-Name` 写死 | 改为可配置（默认仍 `chrome`，与抓包一致） | `browser_name` |
| §2.7 浏览器自动头缺失 | 补 `sec-ch-ua` / `sec-ch-ua-mobile` / `sec-ch-ua-platform` / `sec-fetch-site` / `sec-fetch-mode` / `sec-fetch-dest` / `priority` / `accept-language` / `accept-encoding` | `sec_ch_ua` / `accept_language` / `accept_encoding` |
| §2.6 `et` 硬编码 / locale 写死 | Referer 默认改为扩展路径 + `_host_Info`，**不再携带 `et`**（`et` 是宿主导航时附加、前端 JS 不读取、也无公开签发接口，插件无法自行获取）；locale 与扩展 PID 可配置；Referer 可整项关闭 | `referer` / `extension_pid` / `host_info_locale` |
| §2.3 `context_management` 缺省 | 客户端未提供（或为空）且开关开启（**默认开启**）时，注入 `[{type:"compaction", compact_threshold:<阈值>}]`；客户端显式提供时一律透传不改写 | `context_management` / `context_management_compact_threshold` |
| §2.5 会话链路未实现 | 接入**核心**链路：`GET /responses/access` → `GET /accounts/check` → `POST /arc/executors/register` → `POST .../ready`，之后周期性 `heartbeat`、重连时 `bootstrap`；任一环节失败都**不影响** `/responses` | `session_call_chain` / `arc_heartbeat_seconds` |
| `task_id`/`turn_id` 生成 | 默认改用真实客户端的 **UUIDv7** 仿真（48 位毫秒时间戳 + 版本位 7 + 单调计数器 + 随机尾），与真实客户端同为**时间序且带随机**；同会话 `task_id` 稳定、同轮次 `turn_id` 稳定、不同会话/轮次互不相同。设为 false 回退确定性 uuidV5（见 §5.4） | `simulate_task_turn_ids` |

新增/改动文件：`types.go`（字段·默认值·规范化·clone）、`upstream.go`（头构造·装配 id 分配器）、`protocol.go`（`context_management` 注入·`task_id`/`turn_id` 二选一生成）、`client_ids.go`（新增 UUIDv7 生成器与 id 缓存）、`service.go`（会话管理接入·`configure` 重置）、`arc.go`（新增会话链路）、`config.example.yaml`（文档化）。

### 5.2 会话链路细节

- 仅在**已通过 ABI 配置**（真实 CPA 会调用 `plugin.register`）且请求带 `host_callback_id` 时运行，避免零值 `Service` 产生意外宿主回调。
- 会话按 `base URL + 账户` 复用；`configure` 后旧会话作废并重建。
- 每个子调用失败只记录并返回，绝不阻断 `/responses`（与 §5.1 一致）。
- `arc_heartbeat_seconds = 0` 关闭心跳（仍会 register/ready）。

### 5.3 关于 `accept-encoding` 的取舍

真实抓包为 `gzip, deflate, br, zstd`（或 `br, gzip, deflate`）。本项目默认 **`identity`**：宿主 `http.do` 不会替插件解压“插件显式请求的”压缩正文，发送压缩请求头会得到无法解析的二进制体。该项可配置，确认宿主会解压后可改回真实值。

### 5.4 `task_id`/`turn_id`：从确定性 uuidV5 改为仿真真实 UUIDv7

**真实逻辑**（`docs/basispoints-protocol.md` §6.1 的 `metadata` 构造；实现见 chat.openai.com bundle）：

- `task_id`、`turn_id` 都是 **UUIDv7**（RFC 9562）：前 48 位毫秒时间戳（大端）、第 6 字节高 4 位版本 `7`、第 8 字节高 2 位变体 `10`、其余随机。
- 生成器保留**模块级状态**（上次毫秒 `W0` / `rand_a` / `rand_b`），时间未前进时自增计数器（`rand_b → rand_a → 毫秒进位`），因此同一毫秒内的 id 仍严格单调。
- 抓包佐证：同一页面会话内 `rand_a` 恒定（`755d`）、`rand_b` 段缓慢自增（`9ed8 → 9ed9`）、末尾 6 字节每次全新；时间戳段随抓包时间单调递增，解码 `01a0e76bcdf8` ≈ 抓包日期。
- 语义：`task_id` 一个任务/会话一个（跨轮次稳定），`turn_id` 每个用户轮次新铸一个。
- 扩展自身不生成这两个 id，只把宿主的 `taskId`/`turnId` 透传进 `metadata`；缺失即抛错。

**本项目实现**（`client_ids.go`）：

- 新增 `uuidV7Generator` 复刻上述布局与单调计数器，保证：版本 `7`、变体 `10`、48 位毫秒时间戳（单调不减，时钟回拨也不倒退）、末尾随机。
- `clientIDAllocator` 用有界缓存（上限 4096，超出时**淘汰最久未使用**的键，保证活跃会话的 `task_id` 不会被挤掉后重铸）保存「键 → 已铸造 id」，从而在**随机**的前提下仍保持：
  - `task_id`：按会话键（`prompt_cache_key`/`session_id`，否则首条 `input` 项指纹）稳定；
  - `turn_id`：按「会话 + 截至最后一条 user 消息的输入前缀指纹」稳定——同一轮次的多次 agent iteration 共用同一个 `turn_id`，新轮次则新铸。
- 开关 `simulate_task_turn_ids`（**默认开启**）；设为 `false` 时退回旧的确定性 uuidV5（版本 `5`，同输入恒得同一 id）。两条路径都由 `configure` 重置缓存。

**与关闭模拟的差异**：仿真路径的 id 是随机铸造的，只在缓存有效期内对同一会话/轮次保持稳定（进程重启或 `configure` 后会重铸）；uuidV5 回退路径是**纯函数**，同一输入任何时候都得同一 id（幂等、便于重放去重，但不是 v7）。两者的共同点是「同一会话一个 `task_id`、同一轮次一个 `turn_id`」，与真实客户端语义一致。

**不涉及的两件事**：`prompt_cache_key` 是实现独立的字段（来自客户端 session 键），与 `task_id`/`turn_id` 无关；附件上传缓存以**图片字节的 sha256** 为键，也与这两个 id 无关——本次改动都不影响。

---

## 6. 刻意不改（按需求锁定）

| 项 | 原因 |
|---|---|
| §2.1 `bps_tools_version_id` 默认值 | 需求明确锁定：不改。仍为“配置非空才发送”。 |
| §2.4 `reasoning_effort` 取值集合 | 需求明确锁定：不改。 |
| §3 转译层（`run_officejs` 中继等） | 需求明确锁定：保持现有转译逻辑不变。 |
| §2.2 UA/平台/品牌**默认值**仍为 macOS + Chrome | 默认值取自本次 HAR 抓包；已全部可配置，Windows/Edge 用户可通过配置覆盖。未实现“由 UA 自动推导 Browser-Name”，以保持简单、可预期。 |
| §2.8 `x-stainless-*` 限定 `/responses` | 影响极小，未改。 |
| §2.9 `Office-Host-Version` | 抓包未出现，无需改。 |
| §2.5 UI 类端点（标题/建议/技能/连接器/ARC Client Sync WS） | 超出“核心会话链路”范围，未实现。 |
