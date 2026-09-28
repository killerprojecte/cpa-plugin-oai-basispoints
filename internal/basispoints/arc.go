package basispoints

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// 会话链路：网页版插件在 /responses 之前会先建立一条会话（session）——
//   - GET  /responses/access?include_models=true   访问检查 + 模型目录
//   - GET  /accounts/check                         账号检查
//   - POST /arc/executors/register                 注册 ARC 执行器，拿到 executor_session_id
//   - POST /arc/executors/{id}/ready               标记就绪
//   - POST /arc/executors/{id}/heartbeat           每 60s 心跳保活
//   - POST /arc/executors/{id}/bootstrap           心跳失效时刷新会话
//
// 本插件是 headless 代理，不实现 ARC 控制 WebSocket（远程控制）与 UI 端点；这里只补齐
// 上述“建会话 + 保活”的调用链路，使上游看到的调用序列与网页版一致。
//
// 该链路是**尽力而为**的：任何一步失败都不影响 /responses，也不伪造成功；失败会在下次
// 请求时重试。宿主 HTTP 回调（host_callback_id）只在执行器调用期间有效，因此链路按需
// 在请求内联执行，而不是后台常驻协程。

// arcSupportedTools 是网页版 Excel 插件注册时声明的客户端工具集（抓包实测）。
var arcSupportedTools = []map[string]string{
	{"name": "read_ranges", "version": "1"},
	{"name": "search_workbook", "version": "1"},
	{"name": "list_items", "version": "1"},
	{"name": "write_range", "version": "1"},
	{"name": "clear_range", "version": "1"},
	{"name": "update_sheet", "version": "1"},
	{"name": "update_workbook", "version": "1"},
	{"name": "copy_range_to", "version": "1"},
	{"name": "read_range_image", "version": "1"},
	{"name": "run_officejs", "version": "1"},
	{"name": "read_sheets_metadata", "version": "1"},
	{"name": "resize_range", "version": "1"},
	{"name": "update_sheet_view", "version": "1"},
	{"name": "format_range", "version": "1"},
	{"name": "chart", "version": "1"},
	{"name": "table", "version": "1"},
	{"name": "pivot_table", "version": "1"},
}

// arcSession 是一条已建立的会话（按凭据区分）。
type arcSession struct {
	mu         sync.Mutex
	executorID string
	authMode   string
	lastBeat   time.Time
	lastAccess time.Time
}

// sessionManager 按凭据保存会话。
type sessionManager struct {
	mu       sync.Mutex
	sessions map[string]*arcSession
}

func newSessionManager() *sessionManager {
	return &sessionManager{sessions: map[string]*arcSession{}}
}

func (m *sessionManager) reset() {
	m.mu.Lock()
	m.sessions = map[string]*arcSession{}
	m.mu.Unlock()
}

// acquire 返回该 key 对应的会话，不存在则创建一个空壳（仍需完成 register/ready）。
func (m *sessionManager) acquire(key string) *arcSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	if session, ok := m.sessions[key]; ok {
		return session
	}
	session := &arcSession{}
	m.sessions[key] = session
	return session
}

func (m *sessionManager) drop(key string) {
	m.mu.Lock()
	delete(m.sessions, key)
	m.mu.Unlock()
}

// ensureSession 在 /responses 之前建立/保活会话链路。尽力而为：不返回错误，失败静默重试。
func (s *Service) ensureSession(request ExecutorRequest, c credential, cfg Config) {
	// 只在具备宿主回调上下文的真实执行器调用中执行（CPA 总会提供 host_callback_id）。
	if request.HostCallbackID == "" || !cfg.sessionCallChain() {
		return
	}
	key := sessionKey(c)
	session := s.sessions.acquire(key)
	session.mu.Lock()
	defer session.mu.Unlock()

	now := time.Now()
	if session.executorID == "" {
		if s.registerSession(request, c, cfg, session, now) != nil {
			// 注册失败：保留空壳，下次请求重试。
			s.sessions.drop(key)
		}
		return
	}
	// 周期性刷新 access 目录（与网页版约 60s 的轮询一致，取心跳间隔的 5 倍以降低开销）。
	interval := cfg.arcHeartbeatInterval()
	if interval <= 0 {
		return
	}
	if now.Sub(session.lastAccess) >= 5*interval {
		session.lastAccess = now
		s.arcProbe(request, c, cfg, "/responses/access?include_models=true")
	}
	if now.Sub(session.lastBeat) < interval {
		return
	}
	session.lastBeat = now
	if s.arcLifecycle(request, c, cfg, session.executorID, "/heartbeat") {
		return
	}
	// 心跳失败：尝试 bootstrap 刷新；仍失败则丢弃会话，下次请求重新注册。
	if s.arcLifecycle(request, c, cfg, session.executorID, "/bootstrap") {
		_ = s.arcLifecycle(request, c, cfg, session.executorID, "/ready")
		return
	}
	s.sessions.drop(key)
}

// registerSession 执行 access + accounts/check + register + ready。
func (s *Service) registerSession(request ExecutorRequest, c credential, cfg Config, session *arcSession, now time.Time) error {
	s.arcProbe(request, c, cfg, "/responses/access?include_models=true")
	s.arcProbe(request, c, cfg, "/accounts/check")
	body, err := json.Marshal(map[string]any{
		"surface":         "excel",
		"document_id":     documentID(c),
		"document_title":  "ChatGPT.xlsx",
		"supported_tools": arcSupportedTools,
	})
	if err != nil {
		return err
	}
	var response upstreamResponse
	if err := s.arcCall(request, c, cfg, http.MethodPost, "/arc/executors/register", body, &response); err != nil {
		return err
	}
	var result struct {
		OK                bool   `json:"ok"`
		ExecutorSessionID string `json:"executor_session_id"`
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 ||
		json.Unmarshal(response.Body, &result) != nil || strings.TrimSpace(result.ExecutorSessionID) == "" {
		return fail(502, "arc_register", "Basis Points ARC register did not return an executor session")
	}
	session.executorID = strings.TrimSpace(result.ExecutorSessionID)
	session.authMode = c.AuthMode
	session.lastBeat = now
	session.lastAccess = now
	s.arcLifecycle(request, c, cfg, session.executorID, "/ready")
	return nil
}

// arcProbe 发起一次 GET 探针（access / accounts/check），忽略结果。
func (s *Service) arcProbe(request ExecutorRequest, c credential, cfg Config, path string) {
	var response upstreamResponse
	_ = s.arcCall(request, c, cfg, http.MethodGet, path, nil, &response)
}

// arcLifecycle 调用 /arc/executors/{id}/{action}，返回上游 ok 是否为真。
func (s *Service) arcLifecycle(request ExecutorRequest, c credential, cfg Config, executorID, action string) bool {
	path := "/arc/executors/" + url.PathEscape(executorID) + action
	var response upstreamResponse
	if err := s.arcCall(request, c, cfg, http.MethodPost, path, nil, &response); err != nil {
		return false
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return false
	}
	var result struct {
		OK bool `json:"ok"`
	}
	if json.Unmarshal(response.Body, &result) != nil {
		// 正文缺 ok 字段时，2xx 视为成功。
		return true
	}
	return result.OK
}

// arcCall 经宿主执行一次非流式 HTTP 请求；无正文时不发送 Content-Type。
func (s *Service) arcCall(request ExecutorRequest, c credential, cfg Config, method, path string, body []byte, out *upstreamResponse) error {
	base, err := apiBaseURL(cfg)
	if err != nil {
		return err
	}
	headers := authHeaders(c, cfg, false)
	if len(body) == 0 {
		headers.Del("Content-Type")
	}
	payload := map[string]any{
		"host_callback_id": request.HostCallbackID,
		"method":           method,
		"url":              base + path,
		"headers":          headers,
		"body":             body,
	}
	return s.guardedDo(request, c.AccessToken, payload, out)
}

// apiBaseURL 从 responses_url 去掉末尾的 /responses 段，得到 API 基址。
func apiBaseURL(cfg Config) (string, error) {
	u, err := url.Parse(strings.TrimSpace(cfg.ResponsesURL))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fail(500, "invalid_config", "responses_url is not an absolute URL")
	}
	base := *u
	base.RawQuery, base.Fragment = "", ""
	path := strings.TrimRight(base.Path, "/")
	if strings.HasSuffix(path, "/responses") {
		path = strings.TrimSuffix(path, "/responses")
	}
	base.Path = path
	return strings.TrimRight(base.String(), "/"), nil
}

// sessionKey 按凭据区分会话。
func sessionKey(c credential) string {
	return c.AuthMode + "|" + c.AccountID + "|" + c.AccountUserID
}

// documentID 为无工作簿的代理合成一个稳定的文档标识（同一凭据恒定）。
func documentID(c credential) string {
	return uuidV5("cpa-oai-basispoints/document/" + c.AccountUserID + "|" + c.AccountID)
}
