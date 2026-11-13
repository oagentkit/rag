package rag

// 本文件钉 oagentkit/rag#122 的回归：OpenAIEmbedder 绕过构造器的零值形态
// client==nil——batchSize 注释承诺「兼容绕过构造器直接构造的零值形态」却漏了
// client，Embed 首个请求在 client.Do 处 nil deref panic（进程崩，不是错误）。
//
// 修复面 = client 懒兜底；零值形态的 dimension 是私有字段、字面量设不了，
// 维度校验仍会给出**普通错误**（可排查、不崩进程）——从 panic 到 error 的
// 形态变化正是本单要钉的。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// TestZeroValueEmbedderLazilyInitsClient 字面量构造（导出字段合法）→ Embed
// 首个请求不再 nil deref panic：HTTP 往返真的发生（client 懒兜底 30s 缺省
// 且 memoize），失败也只允许是维度校验的普通错误。
func TestZeroValueEmbedderLazilyInitsClient(t *testing.T) {
	const dim = 4
	srv, reqs := newEmbedServer(t, dim)
	e := &OpenAIEmbedder{BaseURL: srv.URL + "/v1", APIKey: "k", Model: "m"}
	if e.client != nil {
		t.Fatal("字面量构造应为零值 client——修复面就是它")
	}

	// 修复前：此处 httpjson.DoJSON(ctx, nil, ...) → client.Do nil deref panic。
	_, err := e.Embed(context.Background(), []string{"hi", "there"})
	if len(*reqs) != 1 {
		t.Fatalf("请求应真的发出（client 懒兜底生效），实际 %d 次——0 次说明没走到 HTTP、非零异常说明崩在别处", len(*reqs))
	}
	if err != nil && !strings.Contains(err.Error(), "维度") {
		t.Fatalf("除维度校验（零值 dimension=0 的既有语义）外不应有其它错误: %v", err)
	}
	if e.client == nil {
		t.Fatal("首调后 client 应已懒兜底")
	}
	if e.client.Timeout != defaultEmbedderTimeout {
		t.Fatalf("零值兜底超时 = %v，应为 %v", e.client.Timeout, defaultEmbedderTimeout)
	}
	// memoize：后续调用复用同一 client（连接池不因零值形态丢失）
	if a, b := e.httpClient(), e.httpClient(); a != b {
		t.Fatal("兜底 client 应 memoize，不应每次新建")
	}
}

// TestZeroValueEmbedderConcurrentFirstUse 并发首调同一零值形态：懒初始化在
// 锁内完成，-race 下不得 data race（AGENTS 并发纪律）；每 goroutine 的请求
// 都要真的发出（任何一个 panic 会直接击穿测试进程）。
func TestZeroValueEmbedderConcurrentFirstUse(t *testing.T) {
	const dim = 4
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		var req embedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		data := make([]map[string]any, len(req.Input))
		for i := range req.Input {
			data[i] = map[string]any{"embedding": make([]float32, dim), "index": i}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer srv.Close()

	e := &OpenAIEmbedder{BaseURL: srv.URL + "/v1", APIKey: "k", Model: "m"}
	const goroutines = 8
	var wg sync.WaitGroup
	errCh := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := e.Embed(context.Background(), []string{"x"})
			errCh <- err
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		// 允许维度校验的普通错误（零值 dimension=0）；panic 会击穿进程，
		// data race 会被 -race 拦下——两者都不以 error 形态出现。
		if err != nil && !strings.Contains(err.Error(), "维度") {
			t.Errorf("并发首调除维度校验外不应有其它错误: %v", err)
		}
	}
	if got := hits.Load(); got != goroutines {
		t.Fatalf("应发出 %d 次请求，实得 %d（懒初始化竞态会让部分请求没发出）", goroutines, got)
	}
}
