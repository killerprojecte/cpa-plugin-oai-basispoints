package basispoints

import (
	"strings"
	"testing"
)

// normalized 返回补齐默认值后的配置副本。
func normalized(t *testing.T, cfg Config) Config {
	t.Helper()
	if err := cfg.normalize(); err != nil {
		t.Fatal(err)
	}
	return cfg
}

// Referer 默认使用扩展页面路径，且不含无法自行获取的 et 权益令牌。
func TestRefererDefaultsToExtensionPathWithoutEt(t *testing.T) {
	cfg := defaultConfig()
	referer := refererValue(cfg)
	if strings.Contains(referer, "et=") {
		t.Fatalf("default referer must not carry et token: %q", referer)
	}
	want := "https://bps.openai.com/basispoints/extension/" + DefaultExtensionPID + "/?_host_Info=Excel$Win32$16.01$" + DefaultHostInfoLocale + "$$$$16"
	if referer != want {
		t.Fatalf("referer = %q, want %q", referer, want)
	}
	// 请求头中确实带上默认 Referer。
	if got := authHeaders(credential{AccessToken: "t", AccountID: "a"}, cfg, false); len(got["Referer"]) != 1 || got["Referer"][0] != want {
		t.Fatalf("Referer header = %#v", got["Referer"])
	}
}

// Referer 可通过 none/off/-/false/disable 关闭。
func TestRefererCanBeDisabled(t *testing.T) {
	for _, value := range []string{"none", "off", "-", "false", "disable", "disabled", "OFF", "None"} {
		cfg := defaultConfig()
		cfg.Referer = value
		cfg = normalized(t, cfg)
		if got := refererValue(cfg); got != "" {
			t.Fatalf("referer %q = %q, want empty (disabled)", value, got)
		}
		if headers := authHeaders(credential{AccessToken: "t", AccountID: "a"}, cfg, false); len(headers["Referer"]) != 0 {
			t.Fatalf("Referer header present for %q: %#v", value, headers["Referer"])
		}
	}
}

// 自定义 Referer 原样发送（保留配置覆盖能力）。
func TestRefererCustomOverride(t *testing.T) {
	cfg := defaultConfig()
	cfg.Referer = "https://example.test/basispoints/extension/custom/?et=abc"
	cfg = normalized(t, cfg)
	if got := refererValue(cfg); got != cfg.Referer {
		t.Fatalf("custom referer = %q, want %q", got, cfg.Referer)
	}
}

// 扩展 PID 与 locale 可配置，用于拼装默认 Referer。
func TestRefererHonorsExtensionPIDAndLocale(t *testing.T) {
	cfg := defaultConfig()
	cfg.ExtensionPID = "11111111-2222-3333-4444-555555555555"
	cfg.HostInfoLocale = "zh-CN"
	cfg = normalized(t, cfg)
	referer := refererValue(cfg)
	if !strings.Contains(referer, cfg.ExtensionPID) {
		t.Fatalf("referer missing configured pid: %q", referer)
	}
	if !strings.Contains(referer, "Excel$Win32$16.01$zh-CN$$$$16") {
		t.Fatalf("referer missing configured locale: %q", referer)
	}
}

// 默认 Referer 的源跟随 responses_url。
func TestRefererTracksResponsesOrigin(t *testing.T) {
	cfg := defaultConfig()
	cfg.ResponsesURL = "https://proxy.example.test/basispoints/api/responses"
	cfg = normalized(t, cfg)
	if got := refererValue(cfg); !strings.HasPrefix(got, "https://proxy.example.test/basispoints/extension/") {
		t.Fatalf("referer origin not derived from responses_url: %q", got)
	}
}

// 浏览器请求头对齐真实网页版。
func TestBrowserHeadersAlignedWithWebProfile(t *testing.T) {
	cfg := defaultConfig()
	cfg = normalized(t, cfg)
	h := authHeaders(credential{AccessToken: "t", AccountID: "a"}, cfg, false)

	want := map[string]string{
		"Sec-Ch-Ua":          DefaultSecCHUA,
		"Sec-Ch-Ua-Mobile":   "?0",
		"Sec-Ch-Ua-Platform": `"` + DefaultUAPlatform + `"`,
		"Sec-Fetch-Site":     "same-origin",
		"Sec-Fetch-Mode":     "cors",
		"Sec-Fetch-Dest":     "empty",
		"Priority":           "u=1, i",
		"Accept-Language":    DefaultAcceptLanguage,
		"Accept-Encoding":    DefaultAcceptEncoding,
		"Origin":             "https://bps.openai.com",
	}
	for key, value := range want {
		if got := h[key]; len(got) != 1 || got[0] != value {
			t.Fatalf("header %q = %#v, want %q", key, got, value)
		}
	}
	// 流式请求 Accept 为 SSE。
	streamHeaders := authHeaders(credential{AccessToken: "t", AccountID: "a"}, cfg, true)
	if got := streamHeaders["Accept"]; len(got) != 1 || got[0] != "text/event-stream" {
		t.Fatalf("stream Accept = %#v", got)
	}
}

// Origin 跟随 responses_url。
func TestOriginTracksResponsesURL(t *testing.T) {
	cfg := defaultConfig()
	cfg.ResponsesURL = "https://proxy.example.test/basispoints/api/responses"
	cfg = normalized(t, cfg)
	h := authHeaders(credential{AccessToken: "t", AccountID: "a"}, cfg, false)
	if got := h["Origin"]; len(got) != 1 || got[0] != "https://proxy.example.test" {
		t.Fatalf("Origin = %#v", got)
	}
}

// Browser-Name 与其它浏览器画像字段可配置。
func TestBrowserProfileFieldsConfigurable(t *testing.T) {
	cfg := defaultConfig()
	cfg.BrowserName = "edge"
	cfg.SecCHUA = `"Microsoft Edge";v="153"`
	cfg.AcceptLanguage = "en-US,en;q=0.9"
	cfg.AcceptEncoding = "gzip, deflate, br"
	cfg.UAPlatform = "Windows"
	cfg = normalized(t, cfg)

	h := authHeaders(credential{AccessToken: "t", AccountID: "a"}, cfg, false)
	checks := map[string]string{
		"X-Openai-Internal-Basispoints-Browser-Name":        "edge",
		"X-Openai-Internal-Basispoints-Browser-UA-Platform": "Windows",
		"Sec-Ch-Ua":          cfg.SecCHUA,
		"Accept-Language":    cfg.AcceptLanguage,
		"Accept-Encoding":    cfg.AcceptEncoding,
		"Sec-Ch-Ua-Platform": `"Windows"`,
	}
	for key, value := range checks {
		if got := h[key]; len(got) != 1 || got[0] != value {
			t.Fatalf("header %q = %#v, want %q", key, got, value)
		}
	}
}

// 空值回落到内置默认，避免发出空头。
func TestBrowserProfileEmptyFallsBackToDefaults(t *testing.T) {
	cfg := defaultConfig()
	cfg.BrowserName = "   "
	cfg.SecCHUA = ""
	cfg.AcceptLanguage = ""
	cfg.AcceptEncoding = ""
	cfg = normalized(t, cfg)
	h := authHeaders(credential{AccessToken: "t", AccountID: "a"}, cfg, false)
	if got := h["X-Openai-Internal-Basispoints-Browser-Name"]; len(got) != 1 || got[0] != DefaultBrowserName {
		t.Fatalf("browser name fallback = %#v", got)
	}
	if got := h["Sec-Ch-Ua"]; len(got) != 1 || got[0] != DefaultSecCHUA {
		t.Fatalf("sec-ch-ua fallback = %#v", got)
	}
	if got := h["Accept-Language"]; len(got) != 1 || got[0] != DefaultAcceptLanguage {
		t.Fatalf("accept-language fallback = %#v", got)
	}
	if got := h["Accept-Encoding"]; len(got) != 1 || got[0] != DefaultAcceptEncoding {
		t.Fatalf("accept-encoding fallback = %#v", got)
	}
}
