// Package rag Embedding 接口定义与 OpenAI 兼容实现。
package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/oekit/pkg/httpjson"
)

// Embedder 文本向量化接口。
type Embedder interface {
	// Embed 将文本切片转为向量切片，返回的向量维度应一致。
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	// Dim 返回向量维度。
	Dim() int
}

// defaultEmbedderTimeout 默认单次 embedding 请求超时。
const defaultEmbedderTimeout = 30 * time.Second

// defaultEmbedBatchSize 默认每批 embedding 条数。
//
// 对齐本实现文档点名的 DashScope（百炼）compatible-mode /embeddings 单批上限：
// text-embedding-v3/v4 为 10 条（端点不代调用方拆分，超限直接 4xx），此前
// 缺省 16 让「配百炼 v3」开箱即挂——输入 >10 条时第二批起全部报错（rag#42）。
// 常见端点单批上限：OpenAI 2048、DashScope v3/v4 10、DashScope v1/v2 25；
// 更小批次对所有端点都合法，接入上限更高的提供商可用 WithBatchSize 调大
// 以减少请求轮次。
const defaultEmbedBatchSize = 10

// OpenAIEmbedder OpenAI 兼容 Embedding API 适配器。
// 支持 OpenAI / 百炼 / 任何兼容 /v1/embeddings 端点。
type OpenAIEmbedder struct {
	BaseURL   string // 如 https://api.openai.com/v1 或 https://dashscope.aliyuncs.com/compatible-mode/v1
	APIKey    string
	Model     string // 如 text-embedding-3-small / text-embedding-v3
	dimension int    // 向量维度（需与模型输出一致）

	client *http.Client
	// retryCfg 非 nil 时走 httpjson.DoJSONWithRetry 重试路径；nil = 单次尝试。
	retryCfg *httpjson.RetryConfig
	// batchSize 每批条数；<=0 时 Embed 内部回落 defaultEmbedBatchSize（兼容
	// 绕过构造器直接构造的零值形态）。
	batchSize int
}

// EmbedderOption OpenAIEmbedder 构造选项。
//
// 命名遵循矩阵纪律：不使用裸 Option（多包 co-import 会冲突），统一 XxxOption。
type EmbedderOption func(*embedderConfig)

// embedderConfig 构造期的可变状态。
//
// client 与 timeout 分开存放、构造收尾统一结算：timeout 要覆盖"生效后 client"
// 自身的 Timeout，若在 option 应用阶段就写进 client，注入顺序会影响结果。
type embedderConfig struct {
	client    *http.Client
	timeout   time.Duration
	retryCfg  *httpjson.RetryConfig
	batchSize int
}

// WithHTTPClient 注入自定义 HTTP client：出站守卫 / mTLS / 代理 / 连接池调参的入口。
// 传 nil 视为未设置（忽略）。
//
// 交互规则：
//   - 注入的 client 会被复制一份后使用，调用方持有的对象不被修改——共享 client
//     被就地改 Timeout 会影响其它使用者；Transport / Jar / CheckRedirect 原样保留，
//     连接池与调用方共用。
//   - 只给本选项、不给 WithTimeout 时，完全沿用注入 client 自身的 Timeout
//     （0 表示无超时）。
//   - 与 WithTimeout 同时给出时，timeout 覆盖注入 client 的 Timeout（见
//     NewOpenAIEmbedder 的交互规则）。
func WithHTTPClient(c *http.Client) EmbedderOption {
	return func(cfg *embedderConfig) {
		if c == nil {
			return
		}
		cfg.client = c
	}
}

// WithTimeout 覆盖请求超时（默认 30s）。
//
// d <= 0 视为未设置（忽略）——要取消超时，请注入自带 Timeout: 0 的 client
// 且不调用本选项。
//
// 交互规则：timeout 一律作用于生效后的 client（注入优先、timeout 覆盖其
// Timeout），与 WithHTTPClient 的传入先后顺序无关。
func WithTimeout(d time.Duration) EmbedderOption {
	return func(cfg *embedderConfig) {
		if d <= 0 {
			return
		}
		cfg.timeout = d
	}
}

// WithRetry 开启重试路径（改走 httpjson.DoJSONWithRetry）。
//
// 默认不重试：单次尝试，行为与引入本选项前完全一致。理由：embedding 调用
// 默认单次，重试策略属于调用方的成本/时延决策——重试会放大上游配额消耗与
// 计费，且对不可重试错误（4xx 参数错）空转。
//
// cfg.MaxAttempts < 1 由 httpjson 视为 1（等于不重试）；cfg.Retryable 为 nil 时
// 用 httpjson 默认判定：网络超时或 HTTP 5xx/429 可重试，4xx 与响应解析失败不重试。
// 因此零值 RetryConfig{} 等价于单次尝试。
func WithRetry(cfg httpjson.RetryConfig) EmbedderOption {
	return func(c *embedderConfig) {
		c.retryCfg = &cfg
	}
}

// WithBatchSize 覆盖每批 embedding 条数（默认 defaultEmbedBatchSize=10）。
//
// n <= 0 视为未设置（忽略）。默认值对齐本实现支持的各 /v1/embeddings 端点中
// 最受限的 DashScope text-embedding-v3/v4（单批 10 条，compatible-mode 不代为
// 拆分，超限 4xx，rag#42）；常见端点单批上限：OpenAI 2048、DashScope v3/v4
// 10、DashScope v1/v2 25。接入上限更高的提供商时可按其文档调大以减少请求
// 轮次；调小始终合法。
func WithBatchSize(n int) EmbedderOption {
	return func(cfg *embedderConfig) {
		if n <= 0 {
			return
		}
		cfg.batchSize = n
	}
}

// NewOpenAIEmbedder 创建 OpenAI 兼容 Embedding 客户端。
//
// opts 为可选行为（见 EmbedderOption）：WithHTTPClient 注入自定义 client、
// WithTimeout 覆盖超时、WithRetry 开启重试、WithBatchSize 覆盖每批条数。
// 不传任何 option 时：单次尝试、30s 超时、每批 defaultEmbedBatchSize 条。
//
// 交互规则（client 与 timeout 谁优先）：
//  1. 生效 client：给了 WithHTTPClient 就用它（取副本），否则用默认的
//     &http.Client{Timeout: 30s}；
//  2. 给了 WithTimeout，该 timeout 覆盖生效 client 自身的 Timeout——即
//     **timeout 优先于注入 client 的 Timeout**，且与两个选项的传入顺序无关；
//  3. 只给 WithHTTPClient 时，完全沿用注入 client 的 Timeout（0 = 无超时）。
func NewOpenAIEmbedder(baseURL, apiKey, model string, dim int, opts ...EmbedderOption) *OpenAIEmbedder {
	cfg := &embedderConfig{}
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}

	e := &OpenAIEmbedder{
		BaseURL:   strings.TrimSuffix(baseURL, "/"),
		APIKey:    apiKey,
		Model:     model,
		dimension: dim,
		retryCfg:  cfg.retryCfg,
		batchSize: cfg.batchSize,
	}

	// 结算生效 client：注入优先（取副本，不改调用方持有的对象），
	// timeout 覆盖其 Timeout（与注入顺序无关）。
	client := &http.Client{Timeout: defaultEmbedderTimeout}
	if cfg.client != nil {
		cp := *cfg.client
		client = &cp
	}
	if cfg.timeout > 0 {
		client.Timeout = cfg.timeout
	}
	e.client = client

	return e
}

// Dim 返回向量维度。
func (e *OpenAIEmbedder) Dim() int { return e.dimension }

type embedRequest struct {
	Input []string `json:"input"`
	Model string   `json:"model"`
}

type embedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}

// Embed 将文本切片转为向量切片。
func (e *OpenAIEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	// 每批条数守卫（rag#42）：默认对齐 DashScope v3/v4 单批上限 10（此前 16
	// 超限，输入 >10 条时第二批起全部 4xx）；WithBatchSize 可覆盖。
	batchSize := e.batchSize
	if batchSize <= 0 {
		batchSize = defaultEmbedBatchSize // 零值兜底（绕过构造器直接构造的形态）
	}
	var allEmbeddings [][]float32

	for i := 0; i < len(texts); i += batchSize {
		end := i + batchSize
		if end > len(texts) {
			end = len(texts)
		}
		batch := texts[i:end]

		embeddings, err := e.embedBatch(ctx, batch)
		if err != nil {
			return nil, fmt.Errorf("rag: embedding batch %d-%d: %w", i, end, err)
		}
		allEmbeddings = append(allEmbeddings, embeddings...)
	}

	return allEmbeddings, nil
}

func (e *OpenAIEmbedder) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	body, _ := json.Marshal(embedRequest{Input: texts, Model: e.Model})

	var er embedResponse
	req := httpjson.Request{
		Method: http.MethodPost,
		URL:    e.BaseURL + "/embeddings",
		Body:   body,
		Header: func(h http.Header) {
			h.Set("Content-Type", "application/json")
			h.Set("Authorization", "Bearer "+e.APIKey)
		},
	}

	var err error
	if e.retryCfg != nil {
		// 重试路径：每次尝试重新取 Request（body 已是 []byte，可重复读）。
		err = httpjson.DoJSONWithRetry(ctx, e.client, func() (httpjson.Request, error) {
			return req, nil
		}, *e.retryCfg, &er)
	} else {
		// 默认路径：单次尝试。
		err = httpjson.DoJSON(ctx, e.client, req, &er)
	}
	if err != nil {
		return nil, fmt.Errorf("embedding: %w", err)
	}

	// 按 index 排序（API 保证返回顺序，但防御性排序）
	embeddings := make([][]float32, len(texts))
	for _, d := range er.Data {
		// Index 来自远端 JSON，上下界都不可信（负数下标会 panic）；
		// 被丢弃的槽位由下方维度校验兜底报错
		if d.Index >= 0 && d.Index < len(embeddings) {
			embeddings[d.Index] = d.Embedding
		}
	}

	// 验证维度
	for i, emb := range embeddings {
		if len(emb) != e.dimension {
			return nil, fmt.Errorf("rag: embedding[%d] 维度 %d != 期望 %d", i, len(emb), e.dimension)
		}
	}

	return embeddings, nil
}

func (e *OpenAIEmbedder) String() string {
	return fmt.Sprintf("OpenAIEmbedder(%s, %s, dim=%d)", e.BaseURL, e.Model, e.dimension)
}
