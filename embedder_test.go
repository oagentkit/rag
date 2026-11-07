package rag

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newEmbedServer 起一个 mock /v1/embeddings 服务：把文本 hash 成固定维度向量。
func newEmbedServer(t *testing.T, dim int) (*httptest.Server, *[]embedRequest) {
	t.Helper()
	var requests []embedRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var req embedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		requests = append(requests, req)

		resp := embedResponse{}
		for i, text := range req.Input {
			vec := make([]float32, dim)
			for j, ch := range text {
				vec[j%dim] += float32(ch%97) / 97.0
			}
			resp.Data = append(resp.Data, struct {
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}{Embedding: vec, Index: i})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv, &requests
}

func TestOpenAIEmbedderEmbed(t *testing.T) {
	const dim = 8
	srv, reqs := newEmbedServer(t, dim)
	e := NewOpenAIEmbedder(srv.URL+"/v1", "key-1", "test-model", dim)

	texts := []string{"你好世界", "hello world", "第三个"}
	vecs, err := e.Embed(context.Background(), texts)
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 3 {
		t.Fatalf("应返回 3 个向量，得到 %d", len(vecs))
	}
	for i, v := range vecs {
		if len(v) != dim {
			t.Fatalf("向量[%d] 维度 %d != %d", i, len(v), dim)
		}
	}
	if len(*reqs) != 1 {
		t.Fatalf("3 条文本应单批发送，实际 %d 次请求", len(*reqs))
	}
	if (*reqs)[0].Model != "test-model" {
		t.Fatalf("model = %q", (*reqs)[0].Model)
	}
}

func TestOpenAIEmbedderBatching(t *testing.T) {
	const dim = 4
	srv, reqs := newEmbedServer(t, dim)
	e := NewOpenAIEmbedder(srv.URL+"/v1", "k", "m", dim)

	// 20 条文本 → 2 批（默认批大小 10：对齐 DashScope v3/v4 单批上限，rag#42）
	texts := make([]string, 20)
	for i := range texts {
		texts[i] = "text"
	}
	if _, err := e.Embed(context.Background(), texts); err != nil {
		t.Fatal(err)
	}
	if len(*reqs) != 2 {
		t.Fatalf("20 条应分 2 批，实际 %d 次请求", len(*reqs))
	}
	if len((*reqs)[0].Input) != 10 || len((*reqs)[1].Input) != 10 {
		t.Fatalf("批次大小不符: %d + %d", len((*reqs)[0].Input), len((*reqs)[1].Input))
	}
	// 任何单请求不得超默认上限（百炼 compatible-mode 不代为拆分，超限直接 4xx）
	for i, r := range *reqs {
		if len(r.Input) > defaultEmbedBatchSize {
			t.Fatalf("请求 %d 单批 %d 条超过端点上限 %d", i, len(r.Input), defaultEmbedBatchSize)
		}
	}
}

// TestOpenAIEmbedderBatchSizeOption WithBatchSize 覆盖每批条数（rag#42）：接入
// 单批上限更高的提供商（OpenAI 2048 / DashScope v1-v2 25）时可调大减少请求
// 轮次；非法值（<=0）视为未设置回落默认。
func TestOpenAIEmbedderBatchSizeOption(t *testing.T) {
	const dim = 4
	texts := make([]string, 20)
	for i := range texts {
		texts[i] = "text"
	}

	srv, reqs := newEmbedServer(t, dim)
	e := NewOpenAIEmbedder(srv.URL+"/v1", "k", "m", dim, WithBatchSize(16))
	if _, err := e.Embed(context.Background(), texts); err != nil {
		t.Fatal(err)
	}
	if len(*reqs) != 2 || len((*reqs)[0].Input) != 16 || len((*reqs)[1].Input) != 4 {
		t.Fatalf("WithBatchSize(16) 应 16+4 两批: %d 次请求", len(*reqs))
	}

	// <=0 视为未设置：回落默认 10
	srv2, reqs2 := newEmbedServer(t, dim)
	e2 := NewOpenAIEmbedder(srv2.URL+"/v1", "k", "m", dim, WithBatchSize(0), WithBatchSize(-3))
	if _, err := e2.Embed(context.Background(), texts); err != nil {
		t.Fatal(err)
	}
	if len(*reqs2) != 2 || len((*reqs2)[0].Input) != defaultEmbedBatchSize {
		t.Fatalf("非法 batch size 应回落默认 %d: %+v", defaultEmbedBatchSize, *reqs2)
	}
}

func TestOpenAIEmbedderEmptyInput(t *testing.T) {
	srv, _ := newEmbedServer(t, 4)
	e := NewOpenAIEmbedder(srv.URL+"/v1", "k", "m", 4)
	vecs, err := e.Embed(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if vecs != nil {
		t.Fatal("空输入应返回 nil")
	}
}

func TestOpenAIEmbedderServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid api key"}`))
	}))
	defer srv.Close()

	e := NewOpenAIEmbedder(srv.URL+"/v1", "bad", "m", 4)
	_, err := e.Embed(context.Background(), []string{"x"})
	if err == nil {
		t.Fatal("401 应报错")
	}
}

func TestOpenAIEmbedderDimMismatch(t *testing.T) {
	// 服务返回 dim=4 向量，客户端期望 8 → 报维度错误
	srv, _ := newEmbedServer(t, 4)
	defer srv.Close()

	e := NewOpenAIEmbedder(srv.URL+"/v1", "k", "m", 8)
	_, err := e.Embed(context.Background(), []string{"x"})
	if err == nil {
		t.Fatal("维度不符应报错")
	}
}
