package basispoints

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// config.example.yaml 必须能被插件配置解析器接受，且文档化的新增键确实生效；
// 这能防止示例与 Config 字段脱节（改键名/改取值范围时用例会失败）。
func TestConfigExampleYAMLLoads(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "config.example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Plugins struct {
			Configs map[string]Config `yaml:"configs"`
		} `yaml:"plugins"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("config.example.yaml is not valid YAML: %v", err)
	}
	cfg, ok := doc.Plugins.Configs["oai-basispoints"]
	if !ok {
		t.Fatal("config.example.yaml is missing the oai-basispoints block")
	}
	if err := cfg.normalize(); err != nil {
		t.Fatalf("config.example.yaml rejected by normalize: %v", err)
	}

	if cfg.BrowserName != DefaultBrowserName {
		t.Fatalf("browser_name = %q", cfg.BrowserName)
	}
	if cfg.SecCHUA != DefaultSecCHUA {
		t.Fatalf("sec_ch_ua = %q", cfg.SecCHUA)
	}
	if cfg.AcceptLanguage != DefaultAcceptLanguage {
		t.Fatalf("accept_language = %q", cfg.AcceptLanguage)
	}
	if cfg.AcceptEncoding != DefaultAcceptEncoding {
		t.Fatalf("accept_encoding = %q", cfg.AcceptEncoding)
	}
	if cfg.ExtensionPID != DefaultExtensionPID || cfg.HostInfoLocale != DefaultHostInfoLocale {
		t.Fatalf("extension_pid/host_info_locale = %q/%q", cfg.ExtensionPID, cfg.HostInfoLocale)
	}
	// 示例留空表示用内置默认：扩展路径 + _host_Info，且不含 et。
	if referer := refererValue(cfg); referer == "" || strings.Contains(referer, "et=") {
		t.Fatalf("referer = %q", referer)
	}
	if !cfg.contextManagementEnabled() {
		t.Fatal("context_management must be enabled in the example")
	}
	if cfg.compactThreshold() != DefaultCompactThreshold {
		t.Fatalf("compact threshold = %d", cfg.compactThreshold())
	}
	if !cfg.sessionCallChain() {
		t.Fatal("session_call_chain must be enabled in the example")
	}
	if cfg.arcHeartbeatInterval() != time.Duration(DefaultArcHeartbeatSeconds)*time.Second {
		t.Fatalf("arc heartbeat interval = %v", cfg.arcHeartbeatInterval())
	}
	if !cfg.simulateTaskTurnIDs() {
		t.Fatal("simulate_task_turn_ids must be enabled in the example")
	}
}
