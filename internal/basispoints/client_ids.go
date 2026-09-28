package basispoints

import (
	"crypto/rand"
	"fmt"
	"sync"
	"time"
)

// 真实客户端（chat.openai.com 的 basispoints web app）用 UUIDv7 铸造 task_id/turn_id：
// 前 48 位是毫秒时间戳（大端），第 6 字节高 4 位是版本 7，第 8 字节高 2 位是变体 10，
// 其余位为随机；生成器内部保留「上次毫秒 / rand_a / rand_b」的模块级状态，并在时间未前进时
// 自增计数器（rand_b → rand_a → 毫秒进位），因此同一毫秒内生成的 id 仍严格单调递增。
//
// 本文件按抓包还原该行为：模拟模式下 task_id/turn_id 与真实客户端同为 UUIDv7，具备时间序
// 与随机性；关闭模拟时退回此前的确定性 uuidV5（见 protocol.go）。
//
// 抓包实测：同一页面会话内 rand_a 恒定（如 55d）、rand_b 段缓慢自增（9ed8→9ed9），末尾 6 字
// 节每次全新——正是下面「模块级状态 + 计数器」的表现。

const (
	// uuidV7RandAMax 是 rand_a 的 12 位上限。
	uuidV7RandAMax = 1 << 12
	// uuidV7RandBMax 是 rand_b 的 19 位「高位」段上限（字节 8/9 与字节 10 的高 5 位共用）。
	uuidV7RandBMax = 1 << 19
)

// uuidV7Generator 复刻真实客户端的 UUIDv7 生成器（单调、并发安全）。
type uuidV7Generator struct {
	mu          sync.Mutex
	initialized bool
	lastMsecs   int64
	randA       int64
	randB       int64
}

// next 生成下一个 UUIDv7。now 只用于取毫秒；生成的 id 时间戳保证单调不减。
func (g *uuidV7Generator) next(now time.Time) string {
	g.mu.Lock()
	defer g.mu.Unlock()

	var seed [16]byte
	if _, err := rand.Read(seed[:]); err != nil {
		// crypto/rand 失败时退化为时间派生，保证仍能生成合法 id。
		fallback := uint64(now.UnixNano())
		for index := range seed {
			seed[index] = byte(fallback >> (uint(index%8) * 8))
		}
	}
	msecs := now.UnixMilli()
	if !g.initialized {
		// rand_a / rand_b 首次由随机字节播种（与真实实现 `a[6..10]` 的取位一致）。
		g.randA = int64(seed[6]&0x7f)<<8 | int64(seed[7])
		g.randB = int64(seed[8]&0x3f)<<13 | int64(seed[9])<<5 | int64(seed[10]>>3)
		g.lastMsecs = msecs
		g.initialized = true
	} else if msecs > g.lastMsecs {
		// 时间前进则推进时间戳；rand_a/rand_b 沿用（与真实实现的可见行为一致）。
		g.lastMsecs = msecs
	}
	// 每次调用自增计数器：同一毫秒内仍严格递增，时钟回拨也不会产生重复/倒退。
	g.randB++
	if g.randB > uuidV7RandBMax-1 {
		g.randB = 0
		g.randA++
		if g.randA > uuidV7RandAMax-1 {
			g.randA = 0
			g.lastMsecs++
		}
	}

	var id uuidV7Bytes
	id.pack(g.lastMsecs, g.randA, g.randB, seed)
	return id.String()
}

// uuidV7Bytes 是 UUIDv7 的 16 字节表示。
type uuidV7Bytes [16]byte

// pack 写入 RFC 9562 的 UUIDv7 布局。
func (u *uuidV7Bytes) pack(msecs, randA, randB int64, seed [16]byte) {
	// 0..5：48 位毫秒时间戳（大端）。
	u[0] = byte(msecs >> 40)
	u[1] = byte(msecs >> 32)
	u[2] = byte(msecs >> 24)
	u[3] = byte(msecs >> 16)
	u[4] = byte(msecs >> 8)
	u[5] = byte(msecs)
	// 6..7：版本 7 + rand_a 低 8 位。
	u[6] = byte(randA>>4)&0x0f | 0x70
	u[7] = byte(randA) & 0xff
	// 8..9：变体 10 + rand_b 高位。
	u[8] = byte(randB>>13)&0x3f | 0x80
	u[9] = byte(randB>>5) & 0xff
	// 10：rand_b 低 5 位 + 3 位随机。
	u[10] = byte(randB<<3)&0xff | seed[10]&0x07
	// 11..15：随机尾（每次全新，保证 id 唯一）。
	copy(u[11:], seed[11:])
}

// String 返回 8-4-4-4-12 形式的小写十六进制。
func (u uuidV7Bytes) String() string {
	return fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:16])
}

// Version 返回版本位（UUIDv7 应为 7），仅供测试与自检使用。
func (u uuidV7Bytes) Version() byte { return u[6] >> 4 }

// Variant 返回变体位（RFC 4122/9562 应为 0b10），仅供测试与自检使用。
func (u uuidV7Bytes) Variant() byte { return u[8] >> 6 }

// idCache 是有界的「键 → 已铸造 id」缓存：同一会话的 task_id、同一轮次的 turn_id 只需铸造
// 一次并保持稳定（真实客户端亦如此），避免每次请求都换 id。超出上限时按插入顺序淘汰最旧项。
type idCache struct {
	mu      sync.Mutex
	entries map[string]string
	order   []string
	limit   int
}

const idCacheLimit = 4096

func newIDCache() *idCache {
	return &idCache{entries: map[string]string{}, limit: idCacheLimit}
}

// getOrCreate 返回 key 对应的 id；不存在时用 create 铸造并缓存。命中会刷新该键的最近使用
// 次序，超出上限时淘汰**最久未使用**的键——保证活跃会话的 task_id 不会被别的会话挤掉后重铸
// （task_id 一旦中途改变，上游会把同一个会话当成新任务）。
func (c *idCache) getOrCreate(key string, create func() string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if value, ok := c.entries[key]; ok {
		c.touch(key)
		return value
	}
	value := create()
	c.entries[key] = value
	c.order = append(c.order, key)
	for len(c.order) > c.limit {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.entries, oldest)
	}
	return value
}

// touch 把 key 移到队尾（最近使用）。每次 O(n)，n ≤ limit，开销可忽略。
func (c *idCache) touch(key string) {
	for index, existing := range c.order {
		if existing == key {
			c.order = append(c.order[:index], c.order[index+1:]...)
			break
		}
	}
	c.order = append(c.order, key)
}

// clientIDAllocator 为模拟模式铸造并缓存 task_id/turn_id。
type clientIDAllocator struct {
	generator *uuidV7Generator
	tasks     *idCache
	turns     *idCache
}

func newClientIDAllocator() *clientIDAllocator {
	return &clientIDAllocator{
		generator: &uuidV7Generator{},
		tasks:     newIDCache(),
		turns:     newIDCache(),
	}
}

// reset 丢弃全部已铸造 id（配置变更后旧会话/轮次不再适用）。
func (a *clientIDAllocator) reset() {
	if a == nil {
		return
	}
	*a = *newClientIDAllocator()
}

// taskID 返回该会话稳定的 task_id（一个会话一个，跨轮次不变）。
func (a *clientIDAllocator) taskID(conversation string) string {
	return a.tasks.getOrCreate(conversation, func() string {
		return a.generator.next(time.Now())
	})
}

// turnID 返回该轮次稳定的 turn_id（同一轮次的多次 agent iteration 共用）。
func (a *clientIDAllocator) turnID(conversation, turnFingerprint string) string {
	return a.turns.getOrCreate(conversation+"\x00"+turnFingerprint, func() string {
		return a.generator.next(time.Now())
	})
}
