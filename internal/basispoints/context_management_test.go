package basispoints

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

// context_management 默认开启、可配置关闭，阈值可配置，且经 YAML 配置链路同样生效。
func TestContextManagementConfigPlumbing(t *testing.T) {
	if !defaultConfig().contextManagementEnabled() {
		t.Fatal("context_management must default to enabled")
	}
	if got := defaultConfig().compactThreshold(); got != DefaultCompactThreshold {
		t.Fatalf("default compact threshold = %d, want %d", got, DefaultCompactThreshold)
	}
	off := defaultConfig()
	off.ContextManagement = boolPtr(false)
	if err := off.normalize(); err != nil {
		t.Fatal(err)
	}
	if off.contextManagementEnabled() {
		t.Fatal("context_management=false must disable injection")
	}

	svc := NewService()
	if _, err := svc.Handle("plugin.register", jsonBytes(map[string]any{"config_yaml": []byte("data_dir: \"\"\ncontext_management: false\ncontext_management_compact_threshold: 123456\n")})); err != nil {
		t.Fatal(err)
	}
	if svc.cfg.contextManagementEnabled() {
		t.Fatal("yaml context_management=false was ignored")
	}
	if got := svc.cfg.compactThreshold(); got != 123456 {
		t.Fatalf("yaml compact threshold = %d, want 123456", got)
	}

	// 未显式配置时经 YAML 链路仍默认开启。
	enabledSvc := NewService()
	if _, err := enabledSvc.Handle("plugin.register", jsonBytes(map[string]any{"config_yaml": []byte("data_dir: \"\"\n")})); err != nil {
		t.Fatal(err)
	}
	if !enabledSvc.cfg.contextManagementEnabled() {
		t.Fatal("context_management must stay enabled by default through the config path")
	}
}

// 默认（context_management 开启）且客户端未提供时，注入默认 compaction 策略；关闭该配置
// 时保持旧行为（不发送）。客户端显式提供时一律透传，不改写。
func TestContextManagementWireEncoding(t *testing.T) {
	const injectedPolicy = `[{"compact_threshold":200000,"type":"compaction"}]`
	cases := []struct {
		name        string
		source      string
		enabled     bool
		wantPresent bool
		wantInject  bool
	}{
		{"omitted/default-off", `{"input":"Reply OK"}`, false, false, false},
		{"null/default-off", `{"input":"Reply OK","context_management":null}`, false, false, false},
		{"empty/default-off", `{"input":"Reply OK","context_management":[]}`, false, false, false},
		{"omitted/default-on", `{"input":"Reply OK"}`, true, true, true},
		{"null/default-on", `{"input":"Reply OK","context_management":null}`, true, true, true},
		{"empty/default-on", `{"input":"Reply OK","context_management":[]}`, true, true, true},
		{"explicit-threshold", `{"input":"Reply OK","context_management":[{"type":"compaction","compact_threshold":475000}]}`, true, true, false},
		{"server-threshold", `{"input":"Reply OK","context_management":[{"type":"compaction"}]}`, true, true, false},
		{"invalid-type-not-hidden", `{"input":"Reply OK","context_management":"invalid-policy"}`, true, true, false},
	}
	for _, tc := range cases {
		for _, stream := range []bool{false, true} {
			for _, original := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/stream=%t/original=%t", tc.name, stream, original), func(t *testing.T) {
					service := NewService()
					service.cfg.ContextManagement = boolPtr(tc.enabled)
					calls := 0
					service.SetHost(func(method string, payload any, out any) error {
						calls++
						expectedMethod := "host.http.do"
						if stream {
							expectedMethod = "host.http.do_stream"
						}
						if method != expectedMethod {
							t.Fatalf("method = %s", method)
						}
						encoded := payload.(map[string]any)["body"].([]byte)
						var wire, source map[string]any
						if err := json.Unmarshal(encoded, &wire); err != nil {
							t.Fatal(err)
						}
						if err := json.Unmarshal([]byte(tc.source), &source); err != nil {
							t.Fatal(err)
						}
						policy, present := wire["context_management"]
						if present != tc.wantPresent {
							t.Fatalf("wire policy presence = %t, want %t; policy=%v", present, tc.wantPresent, policy)
						}
						if tc.wantInject {
							if got := string(jsonBytes(policy)); got != injectedPolicy {
								t.Fatalf("injected policy = %s, want %s", got, injectedPolicy)
							}
						} else if present && !reflect.DeepEqual(policy, source["context_management"]) {
							t.Fatalf("policy was changed: %v", policy)
						}
						if entries, ok := policy.([]any); present && ok && len(entries) == 0 {
							t.Fatal("sent empty context_management array")
						}
						if stream {
							*out.(*upstreamStream) = upstreamStream{StatusCode: 200, StreamID: "test-stream"}
						} else {
							*out.(*upstreamResponse) = upstreamResponse{StatusCode: 200}
						}
						return nil
					})
					request := ExecutorRequest{Model: DefaultModelID, Stream: stream, Payload: []byte(tc.source), StorageJSON: jsonBytes(map[string]any{"access_token": "test-access", "account_id": "test-account"})}
					if original {
						request.OriginalRequest = []byte(tc.source)
					}
					body, credential, err := service.prepareRequest(request)
					if err != nil {
						t.Fatal(err)
					}
					if stream {
						_, err = service.upstreamStream(request, body, credential, testGuard(t, service))
					} else {
						_, err = service.upstreamRequest(request, body, credential, false)
					}
					if err != nil {
						t.Fatal(err)
					}
					if calls != 1 {
						t.Fatalf("host calls = %d, want 1", calls)
					}
				})
			}
		}
	}
}
