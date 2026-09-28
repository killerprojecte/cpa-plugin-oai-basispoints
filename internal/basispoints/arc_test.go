package basispoints

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// recordingHost 记录 host.http.do 的目标 URL，并按路径返回网页版形状的响应。
type recordingHost struct {
	mu      sync.Mutex
	urls    []string
	bodies  map[string][]byte
	status  map[string]int
	regBody []byte
}

func (h *recordingHost) call(method string, payload any, out any) error {
	if method != "host.http.do" {
		return nil
	}
	p, _ := payload.(map[string]any)
	url, _ := p["url"].(string)
	body, _ := p["body"].([]byte)
	h.mu.Lock()
	h.urls = append(h.urls, url)
	if h.bodies == nil {
		h.bodies = map[string][]byte{}
	}
	h.bodies[url] = body
	if strings.HasSuffix(url, "/arc/executors/register") {
		h.regBody = append([]byte(nil), body...)
	}
	code := 0
	if h.status != nil {
		code = h.status[url]
	}
	h.mu.Unlock()
	response := out.(*upstreamResponse)
	response.StatusCode = 200
	response.Headers = http.Header{"Content-Type": {"application/json"}}
	if code != 0 {
		response.StatusCode = code
	}
	switch {
	case strings.Contains(url, "/responses/access"):
		response.Body = []byte(`{"allowed":true,"model_catalog":{"models":[]}}`)
	case strings.HasSuffix(url, "/accounts/check"):
		response.Body = []byte(`{"accounts":[],"default_account_id":"acct"}`)
	case strings.HasSuffix(url, "/arc/executors/register"):
		response.Body = []byte(`{"ok":true,"executor_session_id":"bp_arc_e_test","client_sync":{"websocket_url":"wss://x","command_topic_id":"t"}}`)
	case strings.HasSuffix(url, "/ready"), strings.HasSuffix(url, "/heartbeat"), strings.HasSuffix(url, "/bootstrap"):
		response.Body = []byte(`{"ok":true,"executor_session_id":"bp_arc_e_test"}`)
	case strings.HasSuffix(url, "/responses"):
		response.Body = []byte(`{"id":"resp_1","status":"completed","output":[]}`)
	default:
		response.Body = []byte(`{}`)
	}
	return nil
}

func (h *recordingHost) snapshot() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.urls...)
}

func chainExecutorRequest() ExecutorRequest {
	return ExecutorRequest{
		Model:          DefaultModelID,
		HostCallbackID: "cb-1",
		StorageJSON:    jsonBytes(map[string]any{"access_token": "tok", "account_id": "acct-1", "chatgpt_account_user_id": "user-1"}),
		Payload:        jsonBytes(map[string]any{"model": DefaultModelID, "input": "hi"}),
	}
}

// registerWithConfig 让服务进入「已配置」状态（真实 CPA 会调用 plugin.register）。
func registerWithConfig(t *testing.T, svc *Service, configYAML string) {
	t.Helper()
	if _, err := svc.Handle("plugin.register", jsonBytes(map[string]any{"config_yaml": []byte(configYAML)})); err != nil {
		t.Fatal(err)
	}
}

func registerViaABI(t *testing.T, svc *Service) {
	t.Helper()
	registerWithConfig(t, svc, "data_dir: \"\"\n")
}

// 会话链路：在 /responses 之前完成 access + accounts/check + register + ready。
func TestSessionCallChainRunsWebSequence(t *testing.T) {
	svc := NewService()
	host := &recordingHost{}
	svc.SetHost(host.call)
	registerViaABI(t, svc)

	if _, err := svc.execute(jsonBytes(chainExecutorRequest()), false); err != nil {
		t.Fatal(err)
	}
	got := host.snapshot()
	want := []string{
		"/basispoints/api/responses/access?include_models=true",
		"/basispoints/api/accounts/check",
		"/basispoints/api/arc/executors/register",
		"/basispoints/api/arc/executors/bp_arc_e_test/ready",
		"/basispoints/api/responses",
	}
	if len(got) != len(want) {
		t.Fatalf("call sequence = %v, want %v", got, want)
	}
	for i := range want {
		if !strings.HasSuffix(got[i], want[i]) {
			t.Fatalf("call[%d] = %q, want suffix %q", i, got[i], want[i])
		}
	}
	// register 请求体与网页版一致：surface/document_id/document_title/supported_tools。
	var reg map[string]any
	if err := json.Unmarshal(host.regBody, &reg); err != nil {
		t.Fatal(err)
	}
	if reg["surface"] != "excel" || reg["document_title"] != "ChatGPT.xlsx" {
		t.Fatalf("register body surface/title wrong: %s", host.regBody)
	}
	if _, ok := reg["document_id"].(string); !ok || reg["document_id"] == "" {
		t.Fatalf("register body missing document_id: %s", host.regBody)
	}
	tools, _ := reg["supported_tools"].([]any)
	if len(tools) == 0 {
		t.Fatalf("register body missing supported_tools: %s", host.regBody)
	}
}

// 心跳：会话超过心跳间隔后再次请求，插入 heartbeat。
func TestSessionCallChainHeartbeatsAfterInterval(t *testing.T) {
	svc := NewService()
	host := &recordingHost{}
	svc.SetHost(host.call)
	registerWithConfig(t, svc, "data_dir: \"\"\narc_heartbeat_seconds: 60\n")

	if _, err := svc.execute(jsonBytes(chainExecutorRequest()), false); err != nil {
		t.Fatal(err)
	}
	// 回拨 lastBeat，模拟已过心跳间隔。
	svc.sessions.mu.Lock()
	for _, session := range svc.sessions.sessions {
		session.lastBeat = time.Now().Add(-2 * time.Hour)
	}
	svc.sessions.mu.Unlock()

	before := len(host.snapshot())
	if _, err := svc.execute(jsonBytes(chainExecutorRequest()), false); err != nil {
		t.Fatal(err)
	}
	got := host.snapshot()[before:]
	if len(got) == 0 || !strings.HasSuffix(got[0], "/arc/executors/bp_arc_e_test/heartbeat") {
		t.Fatalf("expected heartbeat after interval, got %v", got)
	}
	// 第二次不应重复 register。
	for _, url := range got {
		if strings.HasSuffix(url, "/arc/executors/register") {
			t.Fatalf("session re-registered unexpectedly: %v", got)
		}
	}
}

// 关闭会话链路配置后，只调用 /responses。
func TestSessionCallChainDisabled(t *testing.T) {
	svc := NewService()
	host := &recordingHost{}
	svc.SetHost(host.call)
	registerWithConfig(t, svc, "data_dir: \"\"\nsession_call_chain: false\n")

	if _, err := svc.execute(jsonBytes(chainExecutorRequest()), false); err != nil {
		t.Fatal(err)
	}
	got := host.snapshot()
	if len(got) != 1 || !strings.HasSuffix(got[0], "/basispoints/api/responses") {
		t.Fatalf("disabled chain must only call /responses, got %v", got)
	}
}

// 注册失败不得影响 /responses。
func TestSessionCallChainFailureDoesNotBlockResponses(t *testing.T) {
	svc := NewService()
	host := &recordingHost{status: map[string]int{}}
	svc.SetHost(func(method string, payload any, out any) error {
		if method == "host.http.do" {
			p, _ := payload.(map[string]any)
			if url, _ := p["url"].(string); strings.HasSuffix(url, "/arc/executors/register") {
				host.mu.Lock()
				host.status[url] = 500
				host.mu.Unlock()
			}
		}
		return host.call(method, payload, out)
	})
	registerViaABI(t, svc)

	result, err := svc.execute(jsonBytes(chainExecutorRequest()), false)
	if err != nil {
		t.Fatalf("register failure must not fail /responses: %v", err)
	}
	if objectValue(result) == nil || len(objectValue(result)["Payload"].([]byte)) == 0 {
		t.Fatalf("responses payload missing: %#v", result)
	}
	got := host.snapshot()
	if len(got) == 0 || !strings.HasSuffix(got[len(got)-1], "/basispoints/api/responses") {
		t.Fatalf("responses call missing after chain failure: %v", got)
	}
}

// 未通过 ABI 配置（零值 Service）时不触发会话链路——避免单元测试产生额外宿主回调。
func TestSessionCallChainSkippedBeforeConfigure(t *testing.T) {
	svc := NewService()
	host := &recordingHost{}
	svc.SetHost(host.call)

	if _, err := svc.execute(jsonBytes(chainExecutorRequest()), false); err != nil {
		t.Fatal(err)
	}
	if got := host.snapshot(); len(got) != 1 {
		t.Fatalf("unconfigured service must not run the chain, got %v", got)
	}
}

// 无 host_callback_id 时不运行链路。
func TestSessionCallChainRequiresCallbackID(t *testing.T) {
	svc := NewService()
	host := &recordingHost{}
	svc.SetHost(host.call)
	registerViaABI(t, svc)

	request := chainExecutorRequest()
	request.HostCallbackID = ""
	if _, err := svc.execute(jsonBytes(request), false); err != nil {
		t.Fatal(err)
	}
	if got := host.snapshot(); len(got) != 1 {
		t.Fatalf("missing callback id must skip the chain, got %v", got)
	}
}

func TestAPIBaseURLFromResponsesURL(t *testing.T) {
	cfg := defaultConfig()
	base, err := apiBaseURL(cfg)
	if err != nil || base != "https://bps.openai.com/basispoints/api" {
		t.Fatalf("apiBaseURL = %q err=%v", base, err)
	}
	cfg.ResponsesURL = "https://example.test/custom/responses"
	if base, err := apiBaseURL(cfg); err != nil || base != "https://example.test/custom" {
		t.Fatalf("apiBaseURL custom = %q err=%v", base, err)
	}
}
