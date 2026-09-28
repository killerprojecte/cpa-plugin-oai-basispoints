package basispoints

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// compact 去掉 uuid 中的连字符，便于按字节序比较。
func compactUUID(id string) string { return strings.ReplaceAll(id, "-", "") }

// uuidVersionChar 校验规范 UUID 形式并返回版本位十六进制字符（第 15 个字符）。
func uuidVersionChar(t *testing.T, id string) byte {
	t.Helper()
	if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		t.Fatalf("not a canonical uuid: %q", id)
	}
	return id[14]
}

// uuidVariantNibble 返回第 4 组首字符；RFC 4122/9562 变体 10 时落在 8..b。
func uuidVariantNibble(t *testing.T, id string) byte {
	t.Helper()
	if len(id) != 36 {
		t.Fatalf("not a canonical uuid: %q", id)
	}
	return id[19]
}

// uuidMsecs 解码 UUIDv7 前 48 位的毫秒时间戳。
func uuidMsecs(t *testing.T, id string) int64 {
	t.Helper()
	raw, err := hex.DecodeString(compactUUID(id))
	if err != nil || len(raw) != 16 {
		t.Fatalf("cannot decode uuid %q: %v", id, err)
	}
	return int64(raw[0])<<40 | int64(raw[1])<<32 | int64(raw[2])<<24 |
		int64(raw[3])<<16 | int64(raw[4])<<8 | int64(raw[5])
}

// 配置项默认开启，可显式关闭，经规范化后仍保持。
func TestSimulateTaskTurnIDsConfigDefault(t *testing.T) {
	if !defaultConfig().simulateTaskTurnIDs() {
		t.Fatal("simulate_task_turn_ids must default to enabled")
	}
	off := defaultConfig()
	off.SimulateTaskTurnIDs = boolPtr(false)
	if err := off.normalize(); err != nil {
		t.Fatal(err)
	}
	if off.simulateTaskTurnIDs() {
		t.Fatal("explicit false must disable simulation")
	}
	unset := defaultConfig()
	unset.SimulateTaskTurnIDs = nil
	if err := unset.normalize(); err != nil {
		t.Fatal(err)
	}
	if !unset.simulateTaskTurnIDs() {
		t.Fatal("nil must fall back to enabled")
	}
	// clone 不得共享指针。
	original := defaultConfig()
	cloned := original.clone()
	*cloned.SimulateTaskTurnIDs = false
	if !original.simulateTaskTurnIDs() {
		t.Fatal("clone shares the SimulateTaskTurnIDs pointer")
	}
}

// UUIDv7 布局：版本 7、变体 10、48 位毫秒时间戳；同毫秒内仍严格单调，时钟回拨也不倒退。
func TestUUIDv7GeneratorLayoutAndMonotonicity(t *testing.T) {
	generator := &uuidV7Generator{}
	base := time.Date(2026, 9, 28, 9, 30, 0, 0, time.UTC)

	ids := make([]string, 0, 8)
	for index := 0; index < 8; index++ {
		ids = append(ids, generator.next(base))
	}
	for _, id := range ids {
		if version := uuidVersionChar(t, id); version != '7' {
			t.Fatalf("version = %c, want 7: %s", version, id)
		}
		if variant := uuidVariantNibble(t, id); variant < '8' || variant > 'b' {
			t.Fatalf("variant nibble = %c, want 8..b: %s", variant, id)
		}
		if got := uuidMsecs(t, id); got != base.UnixMilli() {
			t.Fatalf("msecs = %d, want %d", got, base.UnixMilli())
		}
	}
	// 同一毫秒内的连续 id 必须严格递增。
	for index := 1; index < len(ids); index++ {
		if compactUUID(ids[index-1]) >= compactUUID(ids[index]) {
			t.Fatalf("ids not increasing: %s -> %s", ids[index-1], ids[index])
		}
	}

	// 时间前进：时间戳跟随，且仍严格大于上一次。
	later := base.Add(5 * time.Millisecond)
	next := generator.next(later)
	if got := uuidMsecs(t, next); got != later.UnixMilli() {
		t.Fatalf("msecs after advance = %d, want %d", got, later.UnixMilli())
	}
	if compactUUID(next) <= compactUUID(ids[len(ids)-1]) {
		t.Fatalf("id not increasing across time: %s -> %s", ids[len(ids)-1], next)
	}

	// 时钟回拨：仍严格递增，且沿用已推进的毫秒（不倒退）。
	rollback := generator.next(base.Add(-time.Hour))
	if compactUUID(rollback) <= compactUUID(next) {
		t.Fatalf("clock rollback broke monotonicity: %s -> %s", next, rollback)
	}
	if got := uuidMsecs(t, rollback); got != later.UnixMilli() {
		t.Fatalf("rollback msecs = %d, want %d (must not go backwards)", got, later.UnixMilli())
	}
}

// task_id 按会话稳定，turn_id 按轮次稳定；不同会话/轮次互不相同。
func TestClientIDAllocatorTaskAndTurnSemantics(t *testing.T) {
	allocator := newClientIDAllocator()

	taskA := allocator.taskID("conv-a")
	if taskA != allocator.taskID("conv-a") {
		t.Fatal("task_id must stay stable within a conversation")
	}
	taskB := allocator.taskID("conv-b")
	if taskA == taskB {
		t.Fatal("distinct conversations must get distinct task_id")
	}

	turnA1 := allocator.turnID("conv-a", "fp-1")
	if turnA1 != allocator.turnID("conv-a", "fp-1") {
		t.Fatal("turn_id must stay stable within a turn (agent iterations share it)")
	}
	if turnA1 == allocator.turnID("conv-a", "fp-2") {
		t.Fatal("a new turn must get a new turn_id")
	}
	if turnA1 == allocator.turnID("conv-b", "fp-1") {
		t.Fatal("same turn fingerprint in another conversation must not collide")
	}

	for _, id := range []string{taskA, taskB, turnA1, allocator.turnID("conv-a", "fp-2")} {
		if version := uuidVersionChar(t, id); version != '7' {
			t.Fatalf("allocated id is not v7: %s", id)
		}
	}

	// reset 后旧 id 全部作废。
	allocator.reset()
	if allocator.taskID("conv-a") == taskA {
		t.Fatal("reset must discard previously minted ids")
	}
}

// id 缓存有界，超出上限时淘汰最旧项。
func TestIDCacheEvictsOldest(t *testing.T) {
	cache := &idCache{entries: map[string]string{}, limit: 2}
	mint := func() string { return "id" }
	cache.getOrCreate("k1", mint)
	cache.getOrCreate("k2", mint)
	cache.getOrCreate("k3", mint)
	if len(cache.entries) != 2 {
		t.Fatalf("cache size = %d, want 2", len(cache.entries))
	}
	if _, present := cache.entries["k1"]; present {
		t.Fatal("oldest entry was not evicted")
	}
}

// 最近使用过的键不会被淘汰（LRU），即使它插入得更早。
func TestIDCacheKeepsRecentlyUsedKeys(t *testing.T) {
	cache := &idCache{entries: map[string]string{}, limit: 2}
	mint := func() string { return "id" }
	cache.getOrCreate("active", mint)
	cache.getOrCreate("idle", mint)
	cache.getOrCreate("active", mint) // 刷新 active 的最近使用次序
	cache.getOrCreate("new", mint)    // 应淘汰 idle
	if _, present := cache.entries["idle"]; present {
		t.Fatal("least recently used entry was not evicted")
	}
	if _, present := cache.entries["active"]; !present {
		t.Fatal("recently used entry must be kept")
	}
}

// 活跃会话的 task_id 不会因缓存淘汰而中途改变（上游据此关联同一任务）。
func TestTaskIDStableUnderEvictionPressure(t *testing.T) {
	allocator := newClientIDAllocator()
	// 缩小上限以便在测试中触发淘汰。
	allocator.tasks = &idCache{entries: map[string]string{}, limit: 2}

	original := allocator.taskID("conv-a")
	allocator.taskID("conv-b")
	allocator.taskID("conv-b") // 让 conv-b 成为最近使用过
	allocator.taskID("conv-a") // 活跃：刷新 conv-a
	allocator.taskID("conv-c") // 淘汰最久未使用的 conv-b

	if got := allocator.taskID("conv-a"); got != original {
		t.Fatalf("active conversation task_id changed: %s -> %s", original, got)
	}
}

// 模拟模式：prepareResponsesBody 产出 UUIDv7，且 task/turn 在迭代间保持稳定。
func TestPrepareResponsesBodySimulatedIDs(t *testing.T) {
	cfg := defaultConfig()
	ids := newClientIDAllocator()
	base := map[string]any{
		"model":            DefaultModelID,
		"prompt_cache_key": "conv-1",
		"input":            []any{map[string]any{"role": "user", "content": "Inspect the workbook"}},
	}

	first, err := prepareResponsesBody(cloneObject(base), cfg, ids)
	if err != nil {
		t.Fatal(err)
	}
	nextSource := cloneObject(base)
	nextSource["input"] = append(nextSource["input"].([]any),
		map[string]any{"type": "function_call", "call_id": "call_1", "name": "run_officejs", "arguments": "{}"},
		map[string]any{"type": "function_call_output", "call_id": "call_1", "output": "done"},
	)
	second, err := prepareResponsesBody(nextSource, cfg, ids)
	if err != nil {
		t.Fatal(err)
	}

	firstMetadata := objectValue(first["metadata"])
	secondMetadata := objectValue(second["metadata"])
	firstTask, _ := firstMetadata["task_id"].(string)
	secondTask, _ := secondMetadata["task_id"].(string)
	firstTurn, _ := firstMetadata["turn_id"].(string)
	secondTurn, _ := secondMetadata["turn_id"].(string)
	if version := uuidVersionChar(t, firstTask); version != '7' {
		t.Fatalf("task_id version = %c, want 7", version)
	}
	if version := uuidVersionChar(t, firstTurn); version != '7' {
		t.Fatalf("turn_id version = %c, want 7", version)
	}
	if firstTask != secondTask {
		t.Fatalf("task_id changed across iterations: %s -> %s", firstTask, secondTask)
	}
	if firstTurn != secondTurn {
		t.Fatalf("turn_id changed across iterations: %s -> %s", firstTurn, secondTurn)
	}
	if secondMetadata["agent_iteration"] != "2" {
		t.Fatalf("agent_iteration = %v, want 2", secondMetadata["agent_iteration"])
	}

	// 另一会话得到不同 task_id。
	other := cloneObject(base)
	other["prompt_cache_key"] = "conv-2"
	third, err := prepareResponsesBody(other, cfg, ids)
	if err != nil {
		t.Fatal(err)
	}
	thirdTask, _ := objectValue(third["metadata"])["task_id"].(string)
	if thirdTask == firstTask {
		t.Fatal("a different conversation must get a different task_id")
	}
}

// 未传分配器（关闭模拟的路径）时回退确定性 uuidV5。
func TestPrepareResponsesBodyFallsBackToUUIDv5(t *testing.T) {
	cfg := defaultConfig()
	source := map[string]any{
		"model":            DefaultModelID,
		"prompt_cache_key": "conv-1",
		"input":            []any{map[string]any{"role": "user", "content": "hi"}},
	}
	first, err := prepareResponsesBody(cloneObject(source), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := prepareResponsesBody(cloneObject(source), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	firstMetadata := objectValue(first["metadata"])
	secondMetadata := objectValue(second["metadata"])
	if version := uuidVersionChar(t, firstMetadata["task_id"].(string)); version != '5' {
		t.Fatalf("fallback task_id version = %c, want 5", version)
	}
	if version := uuidVersionChar(t, firstMetadata["turn_id"].(string)); version != '5' {
		t.Fatalf("fallback turn_id version = %c, want 5", version)
	}
	// 确定性：同样输入两次得到同样的 id。
	if firstMetadata["task_id"] != secondMetadata["task_id"] || firstMetadata["turn_id"] != secondMetadata["turn_id"] {
		t.Fatal("fallback must be deterministic")
	}
}

// 端到端：默认配置发出 UUIDv7 的 task/turn id；配置关闭后回退 uuidV5。
func TestServiceSimulatedTaskTurnIDsEndToEnd(t *testing.T) {
	cases := []struct {
		name       string
		configYAML string
		version    byte
	}{
		{"default-on", "data_dir: \"\"\nsession_call_chain: false\n", '7'},
		{"explicit-off", "data_dir: \"\"\nsession_call_chain: false\nsimulate_task_turn_ids: false\n", '5'},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewService()
			var mu sync.Mutex
			var bodies [][]byte
			svc.SetHost(func(method string, payload any, out any) error {
				if method != "host.http.do" {
					return nil
				}
				parts, _ := payload.(map[string]any)
				url, _ := parts["url"].(string)
				body, _ := parts["body"].([]byte)
				response := out.(*upstreamResponse)
				response.StatusCode = 200
				response.Headers = http.Header{"Content-Type": {"application/json"}}
				if strings.HasSuffix(url, "/responses") {
					mu.Lock()
					bodies = append(bodies, append([]byte(nil), body...))
					mu.Unlock()
					response.Body = []byte(`{"id":"resp_1","status":"completed","output":[]}`)
					return nil
				}
				response.Body = []byte(`{"ok":true}`)
				return nil
			})
			if _, err := svc.Handle("plugin.register", jsonBytes(map[string]any{"config_yaml": []byte(tc.configYAML)})); err != nil {
				t.Fatal(err)
			}

			request := ExecutorRequest{
				Model:          DefaultModelID,
				HostCallbackID: "cb-1",
				StorageJSON:    jsonBytes(map[string]any{"access_token": "tok", "account_id": "acct-1"}),
				Payload: jsonBytes(map[string]any{
					"model":            DefaultModelID,
					"prompt_cache_key": "conv-1",
					"input":            []any{map[string]any{"role": "user", "content": "hi"}},
				}),
			}
			for index := 0; index < 2; index++ {
				if _, err := svc.execute(jsonBytes(request), false); err != nil {
					t.Fatal(err)
				}
			}

			mu.Lock()
			defer mu.Unlock()
			if len(bodies) != 2 {
				t.Fatalf("responses calls = %d, want 2", len(bodies))
			}
			tasks := make([]string, 0, 2)
			for _, body := range bodies {
				var wire map[string]any
				if err := json.Unmarshal(body, &wire); err != nil {
					t.Fatal(err)
				}
				metadata := objectValue(wire["metadata"])
				taskID, _ := metadata["task_id"].(string)
				turnID, _ := metadata["turn_id"].(string)
				if version := uuidVersionChar(t, taskID); version != tc.version {
					t.Fatalf("task_id version = %c, want %c (%s)", version, tc.version, taskID)
				}
				if version := uuidVersionChar(t, turnID); version != tc.version {
					t.Fatalf("turn_id version = %c, want %c (%s)", version, tc.version, turnID)
				}
				tasks = append(tasks, taskID)
			}
			if tasks[0] != tasks[1] {
				t.Fatalf("task_id not stable across requests: %s -> %s", tasks[0], tasks[1])
			}
		})
	}
}
