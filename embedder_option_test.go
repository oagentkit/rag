package rag

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oekit/pkg/httpjson"
)

// countingRoundTripper 拦截请求并计数的 RoundTripper：用于验证注入的 client 确实被用上。
type countingRoundTripper struct {
	calls atomic.Int64
}

func (c *countingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	c.calls.Add(1)
	return http.DefaultTransport.RoundTrip(req)
}

// writeOKEmbeddings 回一个维度正确的全零向量响应（embedder 只校验维度与数量）。
func writeOKEmbeddings(w http.ResponseWriter, r *http.Request, dim int) {
	var req embedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	resp := embedResponse{}
	for i := range req.Input {
		resp.Data = append(resp.Data, struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		}{Embedding: make([]float32, dim), Index: i})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// newFlakyServer 起一个"前 failTimes 次返 failStatus、之后成功"的 mock 服务。
// 计数用 atomic（httptest handler 跑在独立 goroutine）。
func newFlakyServer(t *testing.T, failTimes int, failStatus, dim int) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n <= int64(failTimes) {
			w.WriteHeader(failStatus)
			_, _ = w.Write([]byte(`{"error":"upstream busy"}`))
			return
		}
		writeOKEmbeddings(w, r, dim)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

// newHangingServer 起一个永不响应的 mock 服务：用于验证超时生效。
// 处理器先读空请求体（让服务端进入后台读、能感知客户端断开），再等请求 ctx
// 结束（客户端超时取消连接会触发），2s 兜底避免测试悬挂。
func newHangingServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// isTimeoutErr 判断错误链里是否有超时错误（http.Client.Timeout 触发时是 url.Error）。
func isTimeoutErr(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// TestOpenAIEmbedderUsesInjectedHTTPClient 注入的 client 确实被用上：
// 自定义 RoundTripper 拦截并计数，服务端也应收到请求。
func TestOpenAIEmbedderUsesInjectedHTTPClient(t *testing.T) {
	const dim = 4
	srv, calls := newFlakyServer(t, 0, 0, dim)

	rt := &countingRoundTripper{}
	e := NewOpenAIEmbedder(srv.URL+"/v1", "k", "m", dim,
		WithHTTPClient(&http.Client{Transport: rt}),
	)

	vecs, err := e.Embed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 2 {
		t.Fatalf("应返回 2 个向量，得到 %d", len(vecs))
	}
	if got := rt.calls.Load(); got != 1 {
		t.Fatalf("注入 client 的 RoundTripper 应被调用 1 次，实际 %d", got)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("服务端应收到 1 次请求，实际 %d", got)
	}
}

// TestOpenAIEmbedderWithTimeoutTriggersTimeout WithTimeout 生效：服务端 hang 时超时中断请求。
func TestOpenAIEmbedderWithTimeoutTriggersTimeout(t *testing.T) {
	srv := newHangingServer(t)
	e := NewOpenAIEmbedder(srv.URL+"/v1", "k", "m", 4, WithTimeout(80*time.Millisecond))

	start := time.Now()
	_, err := e.Embed(context.Background(), []string{"x"})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("服务端 hang 时应触发超时错误")
	}
	if !isTimeoutErr(err) {
		t.Fatalf("应为超时错误，实际 %v", err)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("超时未生效，耗时 %v", elapsed)
	}
}

// TestOpenAIEmbedderTimeoutOverridesInjectedClientTimeout timeout 优先于注入 client 的
// Timeout，且与两个选项的传入顺序无关；调用方持有的 client 不被就地修改。
func TestOpenAIEmbedderTimeoutOverridesInjectedClientTimeout(t *testing.T) {
	cases := []struct {
		name  string
		build func(c *http.Client) []EmbedderOption
	}{
		{
			name: "client 在前 timeout 在后",
			build: func(c *http.Client) []EmbedderOption {
				return []EmbedderOption{WithHTTPClient(c), WithTimeout(80 * time.Millisecond)}
			},
		},
		{
			name: "timeout 在前 client 在后",
			build: func(c *http.Client) []EmbedderOption {
				return []EmbedderOption{WithTimeout(80 * time.Millisecond), WithHTTPClient(c)}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newHangingServer(t)
			injected := &http.Client{Timeout: 30 * time.Second}
			e := NewOpenAIEmbedder(srv.URL+"/v1", "k", "m", 4, tc.build(injected)...)

			start := time.Now()
			_, err := e.Embed(context.Background(), []string{"x"})
			elapsed := time.Since(start)

			if err == nil {
				t.Fatal("服务端 hang 时应触发超时错误")
			}
			if !isTimeoutErr(err) {
				t.Fatalf("应为超时错误，实际 %v", err)
			}
			if elapsed > 3*time.Second {
				t.Fatalf("注入 client 的 30s 超时应被 WithTimeout 覆盖，实际耗时 %v", elapsed)
			}
			if injected.Timeout != 30*time.Second {
				t.Fatalf("调用方持有的 client 不应被就地修改，Timeout = %v", injected.Timeout)
			}
		})
	}
}

// TestOpenAIEmbedderInjectedClientTimeoutHonored 只注入 client 时沿用它自身的 Timeout。
func TestOpenAIEmbedderInjectedClientTimeoutHonored(t *testing.T) {
	srv := newHangingServer(t)
	e := NewOpenAIEmbedder(srv.URL+"/v1", "k", "m", 4,
		WithHTTPClient(&http.Client{Timeout: 80 * time.Millisecond}),
	)

	start := time.Now()
	_, err := e.Embed(context.Background(), []string{"x"})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("服务端 hang 时应触发超时错误")
	}
	if !isTimeoutErr(err) {
		t.Fatalf("应为超时错误，实际 %v", err)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("注入 client 的 80ms 超时未生效，耗时 %v", elapsed)
	}
}

// TestOpenAIEmbedderWithRetryRetriesUntilSuccess 开启重试后，5xx 先失败再恢复正常，最终成功且尝试次数 > 1。
func TestOpenAIEmbedderWithRetryRetriesUntilSuccess(t *testing.T) {
	const dim = 4
	srv, calls := newFlakyServer(t, 2, http.StatusInternalServerError, dim)
	e := NewOpenAIEmbedder(srv.URL+"/v1", "k", "m", dim,
		WithRetry(httpjson.RetryConfig{MaxAttempts: 3, BaseDelay: 10 * time.Millisecond}),
	)

	vecs, err := e.Embed(context.Background(), []string{"x"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 1 || len(vecs[0]) != dim {
		t.Fatalf("向量维度不符: %v", vecs)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("应尝试 3 次（2 次 5xx + 1 次成功），实际 %d", got)
	}
}

// TestOpenAIEmbedderWithRetryRetriesOn429 开启重试后，429 也走重试路径。
func TestOpenAIEmbedderWithRetryRetriesOn429(t *testing.T) {
	const dim = 4
	srv, calls := newFlakyServer(t, 1, http.StatusTooManyRequests, dim)
	e := NewOpenAIEmbedder(srv.URL+"/v1", "k", "m", dim,
		WithRetry(httpjson.RetryConfig{MaxAttempts: 2, BaseDelay: 10 * time.Millisecond}),
	)

	if _, err := e.Embed(context.Background(), []string{"x"}); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("应尝试 2 次（1 次 429 + 1 次成功），实际 %d", got)
	}
}

// TestOpenAIEmbedderDefaultPathDoesNotRetry 默认路径（不传任何 option）单次尝试，不重试。
func TestOpenAIEmbedderDefaultPathDoesNotRetry(t *testing.T) {
	srv, calls := newFlakyServer(t, 99, http.StatusInternalServerError, 4)
	e := NewOpenAIEmbedder(srv.URL+"/v1", "k", "m", 4)

	if _, err := e.Embed(context.Background(), []string{"x"}); err == nil {
		t.Fatal("5xx 应报错")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("默认路径应只尝试 1 次，实际 %d", got)
	}
}

// TestOpenAIEmbedderDefaultClientMatchesLegacyBehavior 默认路径与引入 option 前完全一致：
// 默认 client 超时 30s、不启用重试。
func TestOpenAIEmbedderDefaultClientMatchesLegacyBehavior(t *testing.T) {
	e := NewOpenAIEmbedder("https://api.example.com/v1", "k", "m", 8)
	if e.client == nil {
		t.Fatal("默认 client 不应为 nil")
	}
	if e.client.Timeout != 30*time.Second {
		t.Fatalf("默认超时 = %v，应为 30s", e.client.Timeout)
	}
	if e.retryCfg != nil {
		t.Fatal("默认不应启用重试")
	}
}

// TestOpenAIEmbedderWithRetryZeroConfigDoesNotRetry 零值 RetryConfig 等价单次尝试。
func TestOpenAIEmbedderWithRetryZeroConfigDoesNotRetry(t *testing.T) {
	srv, calls := newFlakyServer(t, 99, http.StatusInternalServerError, 4)
	e := NewOpenAIEmbedder(srv.URL+"/v1", "k", "m", 4, WithRetry(httpjson.RetryConfig{}))

	if _, err := e.Embed(context.Background(), []string{"x"}); err == nil {
		t.Fatal("5xx 应报错")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("零值 RetryConfig 应只尝试 1 次，实际 %d", got)
	}
}

// TestOpenAIEmbedderWithHTTPClientNilFallsBackToDefault nil client 视为未设置，走默认 client。
func TestOpenAIEmbedderWithHTTPClientNilFallsBackToDefault(t *testing.T) {
	const dim = 4
	srv, calls := newFlakyServer(t, 0, 0, dim)
	e := NewOpenAIEmbedder(srv.URL+"/v1", "k", "m", dim, WithHTTPClient(nil))

	if _, err := e.Embed(context.Background(), []string{"x"}); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("服务端应收到 1 次请求，实际 %d", got)
	}
	if e.client == nil || e.client.Timeout != 30*time.Second {
		t.Fatalf("nil client 应回落到默认 30s 超时，实际 %+v", e.client)
	}
}

// TestOpenAIEmbedderErrorKeepsHTTPStatusCode 错误链仍保留 httpjson.StatusError 的状态码
// （重试路径与默认路径都要能 errors.As 取出）。
func TestOpenAIEmbedderErrorKeepsHTTPStatusCode(t *testing.T) {
	cases := []struct {
		name string
		opts []EmbedderOption
	}{
		{name: "默认路径", opts: nil},
		{
			name: "重试路径",
			opts: []EmbedderOption{WithRetry(httpjson.RetryConfig{MaxAttempts: 2, BaseDelay: time.Millisecond})},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := newFlakyServer(t, 99, http.StatusTooManyRequests, 4)
			e := NewOpenAIEmbedder(srv.URL+"/v1", "k", "m", 4, tc.opts...)

			_, err := e.Embed(context.Background(), []string{"x"})
			if err == nil {
				t.Fatal("429 应报错")
			}
			var se *httpjson.StatusError
			if !errors.As(err, &se) {
				t.Fatalf("错误链应保留 StatusError，实际 %v", err)
			}
			if se.Code != http.StatusTooManyRequests {
				t.Fatalf("状态码 = %d，应为 %d", se.Code, http.StatusTooManyRequests)
			}
		})
	}
}
