# OpenAI Basis Points（bps.openai.com）完整通讯协议

> 本文档以**扩展前端 HTML/JS 源码为权威来源**逆向整理，HAR 抓包仅用于交叉验证与补齐时序。
> 权威源码：
> - 页面：`GET /basispoints/extension/360590d7-f8f9-4d88-bf75-0edfe0a4b9f3/?et=…&_host_Info=…`
> - 客户端信息头构造：`/assets/client-info-BZSGgJF-.js`（导出 `IE()`）
> - 协议实现主体：`/assets/x-square-DUrhLSGN.js`（含 `ui`/`H6`/`BA`/`FL`/`Fee`/`tQt`、ARC 客户端、Responses transport、ARC Client Sync）
> - 沙箱桥：`/assets/sandbox-CcBF7rk2.js`、`/sandbox/?officejsSandbox=1`
>
> 抓包文件：`chat.openai.com_2026_09_28_19_18_38.har`（Reqable 3.2.23，Windows / Excel 网页插件）

---

## 1. 总体结构

Basis Points 是 ChatGPT Excel/PowerPoint/Word 插件（`basispoints-*-plugin`）的后端。一次完整会话涉及三条链路：

1. **静态资源链路**：加载 `extension/<pid>/` 页面与其 Vite 打包资源。
2. **控制链路（ARC / Client Sync）**：注册 executor → 通过 `wss://ws.chatgpt.com/...` 订阅 command topic → 心跳保活。用于“Codex 远程控制工作簿”。
3. **数据链路（Responses）**：`/basispoints/api/responses`（SSE，或 WebSocket），承载模型对话与工具调用。

外部依赖（Cloudflare）会在所有请求上附加/校验 `cf_clearance`、`__cf_bm` 等 Cookie。

---

## 2. Base URL 与路径推导

常量（`x-square.js`）：

```js
S5e="http://localhost:3000"
jG ="/basispoints/api"       // chatgpt 模式基路径
NG ="/basispoints/api-key"   // api_key 模式基路径
fT ="/api-key"; HYt="/v1"; UYt="/v1/api-key"
$Yt="https://bps.openai.com"; ZYt="bps.openai.com"; qYt="chatgpt.com"
WYt="X-Basispoints-Auth-Mode"
x5e="X-Openai-Internal-Basispoints-Client-Device-Id"
```

`Fee(authMode, override)` 决定 baseURL：

- 若传入 override（trim 后非空）→ api_key 模式追加 `/api-key`，chatgpt 模式原样使用；
- 否则按当前页面 `location` 推导：
  - hostname === `bps.openai.com` → `window.location.origin`
  - hostname === `chatgpt.com` → `https://bps.openai.com`
  - basePath = chatgpt 模式 `/basispoints/api`，api_key 模式 `/basispoints/api-key`
- 兜底：`https://bps.openai.com/basispoints/api`

`FL(path, baseUrl, authMode)` = `baseUrl` + `path`（相对路径自动补前导 `/`）。
api_key 模式额外做路径重写（`GYt`）：

| 逻辑路径 | api_key 实际路径 |
|---|---|
| `/auth/api-key/validate` | `/validate` |
| `/responses/access` | `/access` |
| `/responses/models` | `/models` |
| `/responses` | `/responses` |
| `/responses/compact` | `/responses/compact` |
| `/task-title` | `/task-title` |
| `/responses/prompt-versions` | `/prompt-versions` |

**本项目（chatgpt 模式）实际使用的 baseURL = `https://bps.openai.com/basispoints/api`。**

### 环境变量覆盖（仅本地/调试，生产不使用）

`BPS_API_BASE_URL` / `BASISPOINTS_API_BASE_URL` / `API_BASE_URL` 覆盖 base；
`BPS_API_ORIGIN`→`Origin`、`BPS_API_REFERER`→`Referer`、`BPS_API_HOST`→`Host`、`BPS_API_USER_AGENT`→`User-Agent`；
`BPS_API_EXTRA_HEADERS`（JSON 对象）追加任意头；`BPS_LOG_API_REQUESTS` 打开请求日志。

---

## 3. 认证与请求头

### 3.1 认证模式

`HL(token)`：token 以 `sk-` 开头 → `api_key`，否则 → `chatgpt`。本协议使用 **`chatgpt`**。

### 3.2 请求头构造（`BA(headers, token, includeClientInfo, authMode)`）

按顺序：

1. `Authorization: Bearer <access_token>`（`kTe`）
2. `X-Basispoints-Auth-Mode: <authMode>`（`WYt`）
3. api_key 模式：`X-Openai-Internal-Basispoints-Client-Device-Id: <bps_statsig_stable_id>`（`oQt`, `x5e`）
4. chatgpt 模式：从 access_token 的 JWT 声明解析并注入（`nQt`，仅在缺失时设置）：
   - `X-OpenAI-Account-Id: <chatgpt_account_id>`
   - `ChatGPT-Account-ID: <chatgpt_account_id>`
   - `X-OpenAI-Account-User-Id: <chatgpt_account_user_id>`
5. `includeBasispointsClientInfoHeaders === true` 时注入客户端画像头（`kee()`，见 3.3）——**仅当头不存在时设置**。
6. 默认头（`tQt`）：先取环境变量（`Origin`/`Referer`/`Host`/`User-Agent`），再合并 `BPS_API_EXTRA_HEADERS`——均**仅当头不存在时设置**。

> 关键点：**Referer / Origin / User-Agent / Cookie / sec-\* 头均由浏览器（或 Office 宿主）自动附加，JS 不显式设置**。因此“真实协议”的 Referer 就是加载页面的 URL（含 `?et=…&_host_Info=…`），Origin 为 `https://bps.openai.com`。

### 3.3 客户端画像头（`IE()` / `kee()`，逐字段）

对 Excel 桌面宿主（Office Online 页面 + Office 宿主）实测取值：

| 头 | 取值（Excel/Windows/Edge 实测） | 来源 |
|---|---|---|
| `X-Openai-Internal-Basispoints-Client-Product` | `basispoints-excel-plugin` | editor |
| `X-Openai-Internal-Basispoints-Client-Platform` | `excel` | editor |
| `X-Openai-Internal-Basispoints-Client-Agent-Profile` | `excel` | data-bps-agent-profile |
| `X-Openai-Internal-Basispoints-Client-Editor` | `excel` | editor |
| `X-Openai-Internal-Basispoints-Client-Host` | `office` | host 类型 |
| `X-Openai-Internal-Basispoints-Client-Runtime` | `desktop` | Office.context.platform 归类 |
| `X-Openai-Internal-Basispoints-Client-Platform-Class` | `PC` | Office.context.platform |
| `X-Openai-Internal-Basispoints-Office-Host` | `Excel` | Office.context.host |
| `X-Openai-Internal-Basispoints-Office-Platform` | `PC` | Office.context.platform |
| `X-Openai-Internal-Basispoints-Office-Host-Version` | （有则发）Office 版本 | Office.context.diagnostics.hostVersion |
| `X-Openai-Internal-Basispoints-Browser-Name` | `edge` | UA 解析（edge/chrome/firefox/safari/unknown） |
| `X-Openai-Internal-Basispoints-Browser-UA-Platform` | `Windows` | navigator.userAgentData.platform |
| `X-Openai-Internal-Basispoints-Browser-UA-Mobile` | `false` | ua_mobile（仅当定义时发） |
| `X-Openai-Internal-Basispoints-Browser-UA-Brands` | `Microsoft Edge WebView2,Not_A Brand,Chromium,Microsoft Edge` | ua_brands.join(",")（仅当非空） |

规则细节：
- editor 决定 `product`：sheets→`basispoints-sheets-plugin`、powerpoint/word 类推；host 固定 `office`（Excel）或 `apps_script`（Sheets）。
- `Client-Platform-Class`：`client_platform`；Sheets 为 `web`，Office 桌面为 `Office.context.platform`。
- `Browser-Name`/`UA-*` 全部由 `navigator` 推导，**不是硬编码**。
- 值超长会截断到 512 字符。

### 3.4 真实抓包中的完整请求头（以 `/basispoints/api/responses` 为例）

```
:method: POST
:authority: bps.openai.com
:path: /basispoints/api/responses
:scheme: https
content-length: 1198
x-basispoints-auth-mode: chatgpt
x-openai-internal-basispoints-browser-ua-platform: Windows
authorization: Bearer eyJhbGciOiJSUzI1NiIsImtpZCI6…
x-openai-internal-basispoints-office-platform: PC
x-stainless-arch: unknown
sec-ch-ua: "Microsoft Edge WebView2";v="153", "Not_A Brand";v="8", "Chromium";v="153", "Microsoft Edge";v="153"
x-openai-internal-basispoints-browser-ua-brands: Microsoft Edge WebView2,Not_A Brand,Chromium,Microsoft Edge
sec-ch-ua-mobile: ?0
x-stainless-lang: js
accept: text/event-stream
x-openai-internal-basispoints-client-platform-class: PC
content-type: application/json
x-openai-internal-basispoints-client-runtime: desktop
x-stainless-package-version: 6.31.0
x-stainless-runtime-version: 153.0.0
x-openai-internal-basispoints-client-product: basispoints-excel-plugin
x-openai-account-id: 41786728-f01c-4105-a044-aa060f91534b
x-openai-internal-basispoints-client-host: office
x-openai-internal-basispoints-client-editor: excel
sec-ch-ua-platform: "Windows"
x-stainless-os: Unknown
chatgpt-account-id: 41786728-f01c-4105-a044-aa060f91534b
x-stainless-runtime: browser:chrome
x-openai-internal-basispoints-browser-ua-mobile: false
baggage: sentry-environment=production,sentry-public_key=d3e75214397bc4256a94b9b4a591c90b,sentry-trace_id=…,sentry-org_id=33249,sentry-sampled=true,sentry-sample_rand=…,sentry-sample_rate=1
sentry-trace: 469f7dfd42b84262af18a1f161d52e61-b88ae4777e5c6f27-1
x-openai-internal-basispoints-browser-name: edge
x-openai-internal-basispoints-client-platform: excel
x-openai-internal-basispoints-office-host: Excel
x-stainless-retry-count: 0
x-openai-account-user-id: user-fAs1bN6YIaApn656HKWGRPgc__41786728-f01c-4105-a044-aa060f91534b
x-openai-internal-basispoints-client-agent-profile: excel
user-agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/153.0.0.0 Safari/537.36 Edg/153.0.0.0
origin: https://bps.openai.com
sec-fetch-site: same-origin
sec-fetch-mode: cors
sec-fetch-dest: empty
referer: https://bps.openai.com/basispoints/extension/360590d7-f8f9-4d88-bf75-0edfe0a4b9f3/?et=<URL 编码的 et 令牌>&_host_Info=Excel$Win32$16.01$zh-CN$$$$16
accept-encoding: gzip, deflate, br, zstd
accept-language: zh-CN,zh;q=0.9,en;q=0.8,en-GB;q=0.7,en-US;q=0.6
cookie: __cflb=…; _cfuvid=…; __cf_bm=…
priority: u=1, i
```

Referer 的字面完整取值（`et` 为 §10 令牌的 URL 编码形式）：

```
https://bps.openai.com/basispoints/extension/360590d7-f8f9-4d88-bf75-0edfe0a4b9f3/?et=PAByACAAdgA9ACIAMQAiAD4APAB0ACAAYQBpAGQAPQAiAFcAQQAyADAAMAAwADEAMAAyADEANQAiACAAcABpAGQAPQAiAGMAYgBhADkAZgBjADAANgAtADYAZgBjADkALQA0ADkAMgBlAC0AOQA1ADIANQAtADkAMgAzADgANwAwAGEAMwBiADkAMAA5ACIAIABjAGkAZAA9ACIAMgAyADAAQQA4ADUANgBCADkAOQAxADIARAA2ADMAOAAiACAAbwBpAGQAPQAiADAAMAAwADAAMAAwADAAMAAtADAAMAAwADAALQAwADAAMAAwAC0AQgA1ADYAMAAtADAAQQA2AEMAMAAwAEEAQgA3ADEARgA4ACIAIAB0AHMAPQAiADAAIgAgAHMAbAA9ACIAdAByAHUAZQAiACAAZQB0AD0AIgBGAHIAZQBlACIAIABhAGQAPQAiADIAMAAyADYALQAwADkALQAyADgAVAAwADkAOgA0ADQAOgA1ADUAWgAiACAAcwBkAD0AIgAyADAAMgA2AC0AMAA5AC0AMgA4ACIAIAB0AGUAPQAiADIAMAAyADcALQAwADkALQAyADgAVAAwADkAOgA0ADYAOgAwADcAWgAiACAAcwBzAD0AIgAwACIAIAAvAD4APABkAD4ATwBZAFIAcQB0AG8AKwBKAHMAZQB6AG8ASwBGAFIAdABQAGgAUABKAHQAYwBLAFQAYwBXAHAAagBXAEcAZgA0AHkAWgBTAHMAZAByADUASQArADAAawA9ADwALwBkAD4APAAvAHIAPgA%3D&_host_Info=Excel$Win32$16.01$zh-CN$$$$16
```

说明：
- `x-stainless-*` 仅出现在 `/responses`（OpenAI JS SDK 6.31.0 客户端），其它端点没有。
- `baggage` / `sentry-trace` 是 Sentry 链路追踪，非协议必需。
- `sec-ch-ua*` / `sec-fetch-*` / `priority` / `accept-encoding` / `accept-language` / `cookie` 均为浏览器自动添加。
- Referer 的 `_host_Info=Excel$Win32$16.01$<locale>$$$$16` 由 Office 宿主附加，`<locale>` 随客户端语言变化（实测 `zh-CN`）。

### 3.5 401/403 处理

`H6`：若 `requireAuth` 且返回 401/403，且 `isExpired()` 为真，则调用 `refreshIfExpired()` 刷新一次 access token 并用新 token 重发；若 401 且未过期，触发 `onUnauthorized()`（登出）。

---

## 4. 端点清单

> 默认 `method=POST`；`ui()` 会解析 JSON 返回体。除注明外均带 `includeBasispointsClientInfoHeaders`。

| # | Method | Path | 请求体 | 返回 |
|---|---|---|---|---|
| 1 | GET | `/responses/access?include_models=true` | — | access + `model_catalog`（见 §7） |
| 2 | GET | `/responses/models`（api_key） | — | 模型目录 |
| 3 | POST | `/responses` | Responses 请求（§6） | SSE（或 WS） |
| 4 | POST | `/responses/compact` | 压缩请求 | — |
| 5 | POST | `/arc/executors/register` | `{surface,document_id,document_title,supported_tools:[{name,version}]}` | `{ok,executor_session_id,client_sync:{websocket_url,command_topic_id}}` |
| 6 | POST | `/arc/executors/{id}/bootstrap` | 空 | 同上 |
| 7 | POST | `/arc/executors/{id}/ready` | 空 | `{ok,executor_session_id,reason}` |
| 8 | POST | `/arc/executors/{id}/heartbeat` | 空 | `{ok,executor_session_id,reason}` |
| 9 | POST | `/arc/commands/{id}/result` | `{...}` | — |
| 10 | GET | `/arc/settings` | — | `{opted_out}` |
| 11 | POST | `/arc/settings` | `{opted_out}` | `{opted_out}` |
| 12 | POST | `/attachments` | multipart，字段 `file` | `{openai_file_id,filename,content_type}` |
| 13 | POST | `/task-title` | `{task_id,locale,user_message}` | `{title,model}` |
| 14 | POST | `/prompt-suggestions` | `{task_id,locale,model,document_context,onboarding_preferences?}` | `{suggestions,model}` |
| 15 | POST | `/workbook-prompt-suggestions` | 同上（可去 onboarding_preferences 重试） | — |
| 16 | GET | `/accounts/check` | — | `{accounts,default_account_id,account_ordering}` |
| 17 | GET | `/connectors/apps` | — | `{apps:[{id,name,connector_type,chatgpt_enabled,basispoints_enabled,manage_url}]}` |
| 18 | GET | `/skills?prefetch=full&surface=spreadsheet` | —（另有 `x-openai-internal-basispoints-supported-hidden-skill-ids` 头） | `{data:[skill…]}` |
| 19 | POST | `/auth/api-key/validate` | — | — |

`register` 请求体示例（实测）：

```json
{"surface":"excel","document_id":"b4160114-546e-40fe-ab71-0e9d8fab40b0","document_title":"ChatGPT.xlsx",
 "supported_tools":[{"name":"read_ranges","version":"1"},{"name":"search_workbook","version":"1"},{"name":"list_items","version":"1"},{"name":"write_range","version":"1"},{"name":"clear_range","version":"1"},{"name":"update_sheet","version":"1"},{"name":"update_workbook","version":"1"},{"name":"copy_range_to","version":"1"},{"name":"read_range_image","version":"1"},{"name":"run_officejs","version":"1"},{"name":"read_sheets_metadata","version":"1"},{"name":"resize_range","version":"1"},{"name":"update_sheet_view","version":"1"},{"name":"format_range","version":"1"},{"name":"chart","version":"1"},{"name":"table","version":"1"},{"name":"pivot_table","version":"1"}]}
```

`register`/`bootstrap` 返回示例：

```json
{"ok":true,"executor_session_id":"bp_arc_e_6aba3c57b9f08191816e5fe2ac69c170",
 "client_sync":{"websocket_url":"wss://ws.chatgpt.com/p16/ws/user/user-fAs1bN6YIaApn656HKWGRPgc?verify=…",
                "command_topic_id":"arc_commands:executor:bp_arc_e_6aba3c57b9f08191816e5fe2ac69c170"}}
```

---

## 5. 调用顺序与时序

### 5.1 冷启动（插件页面加载）

```
1.  GET  /basispoints/extension/<pid>/?et=<token>&_host_Info=Excel$Win32$16.01$<locale>$$$$16
2.  GET  /basispoints/extension/<pid>/assets/index-u0FoMu0V.js        (+ preload-helper, client-info, x-square, polyfills …)
3.  GET  /basispoints/api/responses/access?include_models=true         # 拉取模型目录/套餐/特性开关
4.  GET  /basispoints/api/accounts/check
5.  GET  /basispoints/api/connectors/apps                              # 与 6/7 并行
6.  GET  /basispoints/api/arc/settings
7.  GET  /basispoints/api/skills?prefetch=full&surface=spreadsheet
8.  POST /basispoints/api/prompt-suggestions                           # 空对话时的建议
```

### 5.2 ARC 控制链路生命周期（`arc_control_transport_enabled === true`）

```
loop:
  registerExecutor()            POST /arc/executors/register      # 无 session 时
  refreshBootstrap(session)     POST /arc/executors/{id}/bootstrap   # 需要刷新 client_sync 时
  connectCommandStream()        WS  client_sync.websocket_url      # 发送 connect+subscribe
  markExecutorReady(session)    POST /arc/executors/{id}/ready
  keepConnected:
     每 60s  heartbeatExecutor()  POST /arc/executors/{id}/heartbeat
  连接关闭 / bootstrap 过期 → 重新 bootstrap 或 register，指数退避重试
```

实测：`register`(10:07:21) → `ready`(10:07:23) → `heartbeat` 每约 60s；重连时先 `bootstrap` 再 `ready`（如 09:52:15/09:52:27 bootstrap → 09:52:30 ready）。

### 5.3 每一轮用户对话

```
1. POST /basispoints/api/task-title      {task_id, locale, user_message}   → 会话标题
2. POST /basispoints/api/responses       SSE / WS response.create          → 模型流
     模型返回工具调用 → 客户端本地执行 → 结果作为 function_call_output 追加到 input → 再次 /responses
     最多 200 个迭代（Hqr=200）
3. 周期 GET /basispoints/api/responses/access?include_models=true           # 约 60s 刷新目录
```

### 5.4 关键时间参数（源码常量）

| 常量 | 值 | 含义 |
|---|---|---|
| `fjn` | `60000` | ARC 心跳间隔 60s |
| `KLn` | `10000` | ARC REST 请求超时 10s |
| `WDn` | `10000` | Client Sync 订阅超时 10s |
| `Hqr` | `200` | 每轮最大工具迭代数 |
| — | `15000` | `/responses/access` 拉取超时 15s |
| `WUr` | `16 MiB` | Responses WS 单帧上限 |
| `eRn` | `300000` | `/arc/settings` staleTime 5min |

---

## 6. `/responses` 请求体与流

### 6.1 请求体构造（agent loop，`basispoints_proxy` 模式）

```js
const body = { model, input, stream: true, store: false };
if (tools) body.tools = tools;
const cm = enableCompact ? contextManagement(model) : undefined;   // [{type:"compaction",compact_threshold:N}]
if (cm) body.context_management = cm;
const metadata = {};
if (taskId) metadata.task_id = taskId;
if (turnId) metadata.turn_id = turnId;
if (toolsVersionId) metadata.bps_tools_version_id = toolsVersionId;
metadata.agent_iteration = String(iteration);      // 字符串
/* 可选：bps_prompt_version_override / bps_prompt_base_override（本地覆盖） */
if (Object.keys(metadata).length) body.metadata = metadata;
if (instructions) body.instructions = instructions;
if (modelMode === "direct") {
    body.include = ["reasoning.encrypted_content"];
    if (effort && effort !== "none") body.reasoning = { effort };
} else {
    body.reasoning_effort = effort ?? "none";
    body.model_selection = modelSelection;          // "explicit"
}
```

实测请求体（Excel，模型 `gpt-5.6-sol`）：

```json
{"model":"gpt-5.6-sol",
 "input":[
   {"type":"message","role":"developer","content":[{"type":"input_text","text":"Runtime context (read-only; client-provided, not authoritative for auth):\n{\"client\":{…},\"document\":{…}}"}]},
   {"type":"message","role":"user","content":[{"type":"input_text","text":"test"}]}],
 "stream":true,"store":false,
 "context_management":[{"type":"compaction","compact_threshold":200000}],
 "metadata":{"task_id":"01a0e76b-cdf8-755d-9ed8-d868aa706354",
             "turn_id":"01a0e76b-cdf7-755d-9ed8-d0324e7e8d58",
             "bps_tools_version_id":"tools-excel-core-2026-06-16-3af59f22",
             "agent_iteration":"1"},
 "reasoning_effort":"medium","model_selection":"explicit"}
```

约束（源码）：`task_id` 与 `turn_id` **必填**，缺失会抛
`Missing taskId/turnId (required by basispoints /responses proxy).`。
`reasoning_effort` 取值集合 `["none","low","medium","high","xhigh"]`，默认取模型目录的 `default_effort`（实测为 `medium`）。

### 6.2 响应头（`/responses`，实测）

```
content-type: text/event-stream; charset=utf-8
cache-control: no-cache
x-openai-internal-basispoints-agent-prompt-sha256: 1ae2c06a…b80b
x-openai-internal-basispoints-agent-tools-sha256: 7be9340a…4e80
x-openai-internal-basispoints-tools-version-id: tools-excel-core-2026-06-16-3af59f22
x-openai-internal-basispoints-prompt-version-id: prompt-basispoints-excel-2026-05-12-083548e9
openai-version: 2020-10-01
x-request-id: <uuid>
openai-processing-ms: <ms>
x-openai-proxy-wasm: v0.1
```

服务端在 `response.created.response.instructions` 返回该 editor 的完整 agent system prompt。

### 6.3 SSE 事件顺序（实测）

```
response.created
response.in_progress
response.output_item.added          (message)
  response.content_part.added
  response.output_text.delta        ×N
  response.output_text.done
  response.content_part.done
response.output_item.done
[若有工具调用] response.output_item.added (function_call) → response.function_call_arguments.delta/done → response.output_item.done
response.completed
```

### 6.4 WebSocket 传输（`responses_websocket_enabled` 为真时）

- URL（`QUr`）：`FL("responses")` 转 `wss:`，追加查询参数：
  - `bps_client_info` = JSON 序列化后的客户端画像头（`kee()`）
  - `bps_auth_mode` = `chatgpt`
  - `bps_ws_affinity` = `<affinity token>`（可选）
  - `bps_control_frames` = `upstream_sent,heartbeat,resume,cancel,server_draining`
- 子协议（`JUr`）：`["responses", "openai-bearer.<access_token>"]`
- 首帧：`{...requestBody, type:"response.create", basispoints_request_id, basispoints_resume_token}`
- 终态事件集合：`error, response.completed, response.failed, response.cancelled, response.incomplete`
- 单帧上限 16 MiB；支持 `resume`（`basispoints.response.resume_token` 帧回传服务端 resume token）与 `cancel`。
- 失败原因集合（如 `websocket_resume_not_found`、`socket_closed_mid_stream`、`proxy_server_draining` 等）触发重连/重放策略；已完成输出绝不重放。

---

## 7. `/responses/access` 返回（模型目录与特性开关）

实测（节选）：

```json
{"minimum_build_version":1785405037,"allowed":true,
 "model_catalog":{"models":[
    {"id":"gpt-5.6-luna","label":"GPT-5.6 Luna","efforts":[{"value":"none",…},{"value":"low",…},{"value":"medium",…},{"value":"high",…},{"value":"xhigh",…}],"default_effort":"medium","free_preview":false},
    {"id":"gpt-5.6-terra",…},{"id":"gpt-5.6-sol",…},
    {"id":"gpt-6-astra","efforts":[{"value":"low"},{"value":"medium"},{"value":"high"},{"value":"xhigh"}],"default_effort":"medium"},
    {"id":"gpt-6-luna",…},{"id":"gpt-6-sol",…}],
   "default_model":"gpt-5.6-sol","source":"chatgpt","restricted_models":[]},
 "plan_type":"pro","auth_mode":"chatgpt","account_scope_id":"user-fAs1bN6YIaApn656HKWGRPgc",
 "onboarding_seen":true,"connectors_enabled":true,"denial_reason":null,
 "features":{"dictation_enabled":true,"skills_enabled":true,
   "prompt_suggestions_enabled":true,"workbook_prompt_suggestions_enabled":true,
   "arc_control_transport_enabled":true,
   "responses_websocket_enabled":false,"responses_websocket_fallback_enabled":true}}
```

- 模型 id 集合（`hFr`/`hE`）：`gpt-5.6*`、`gpt-6-astra/sol/luna` 等；efforts 集合见上。
- 工具版本 id（`wr`）：`tools-excel-core-2026-06-16-3af59f22`（excel）、`tools-spreadsheet-core-2026-05-22-34ba0624`（default/spreadsheet）等。
- `arc_control_transport_enabled` 决定是否启用 §5.2 的 ARC 链路。
- `responses_websocket_enabled=false` 时 `/responses` 走 HTTP SSE（本次抓包即如此）。

---

## 8. ARC Client Sync WebSocket（控制通道）

- 连接 `client_sync.websocket_url`（`wss://ws.chatgpt.com/p16/ws/user/<user>?verify=<签名>`）。
- `open` 时发送一个 **JSON 数组**：

```json
[{"id":"<id1>","command":{"type":"connect"}},
 {"id":"<id2>","command":{"type":"subscribe","topic_id":"arc_commands:executor:<executor_session_id>"}}]
```

- 等待回包中出现 `reply.topic_id === command_topic_id` 视为订阅成功（超时 10s）。
- 之后按 topic 分发工具调用命令；命令结果通过 `POST /arc/commands/{id}/result` 回传。

---

## 9. Cookie 与 Cloudflare

页面加载阶段浏览器携带（同站点）：

```
oai-did, oaicom-stable-id, _ga, _ga_62J2E5SERF, __cflb, _cfuvid, oai-sc, __cf_bm, cf_clearance
```

API 响应会刷新：

```
set-cookie: __cf_bm=<…>; HttpOnly; SameSite=None; Secure; Path=/; Domain=bps.openai.com; Expires=<+30min>
```

非浏览器客户端需自行处理 `cf_clearance`（Cloudflare 反爬）与 `__cf_bm`；`cf_clearance` 与 UA/TLS 指纹绑定。

---

## 10. `et` 权益令牌格式

页面 URL 的 `?et=` 是 **UTF-16LE + base64 + URL 编码** 的 XML：

```xml
<r v="1">
  <t aid="WA200010215" pid="360590d7-f8f9-4d88-bf75-0edfe0a4b9f3"
     cid="220A856B9912D638" oid="00000000-0000-0000-B560-0A6C00AB71F8"
     ts="0" sl="true" et="Free" ad="2026-09-28T09:44:55Z" sd="2026-09-28"
     te="2027-09-28T09:46:07Z" ss="0" />
  <d>OYRqto+JsezoKFRtPhPJtcKTcWpjWGf4yZSsdr5I+0k=</d>
</r>
```

| 属性 | 含义 |
|---|---|
| `aid` | 账号 id（`WA200010215`） |
| `pid` | 插件安装 id，即 URL 中的 `<pid>` 路径段 |
| `cid` | 客户端/安装 id |
| `oid` | 组织 id |
| `sl` | 是否登录 |
| `et` | 权益类型（`Free`/`Plus`/`Pro`…） |
| `ad`/`sd`/`te` | 激活/开始/到期时间 |
| `<d>` | 签名（base64） |

---

## 11. 与本项目实现的差异与对齐

> 本项目 = CPA 插件 `oai-basispoints`。以下“差异”指本项目与上述**真实浏览器/插件协议**的出入。
> **注意：本项目的 run_officejs 中继“转译”层是刻意设计，不在对齐范围内，保持不动。**

（详见 `docs/basispoints-alignment.md`）
