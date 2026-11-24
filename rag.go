// Package rag 知识检索服务。
// 提供两种后端：Local（本地 TF-IDF，本包内）与向量数据库后端（独立子包
// github.com/oagentkit/rag/milvus）。
// 检索算法 = 词元重叠打分（ASCII 词 + 非 ASCII 字母二元组）或向量相似度。
//
// ⚠️ 向量后端在**子包**（oagentkit/rag#156）：本包不再 import 向量 sdk——import
// 本包任何符号（哪怕只用本地索引/混合召回）都不会把向量 sdk 及其传递依赖
// （cockroachdb/errors、sentry-go、gogo/protobuf 等）拖进下游二进制。
// 向量后端的装配入口是 milvus.NewStore(ctx, cfg, embedder)。
package rag

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"

	"github.com/oagentkit/textx/textutil"
)

// Chunk 检索片段。
type Chunk struct {
	ID       string
	Content  string
	Score    float64
	Metadata map[string]string
}

// Filter 检索过滤条件（如 repo、规范文档名）。
type Filter map[string]string

// DefaultTopK 默认检索条数（两后端同口径：<=0 的入参归一到本值）。
const DefaultTopK = 5

// filterKeys 检索过滤字段白名单（包级单源：Local 与向量后端共用——此前白名单
// 只在向量后端侧，Local 对未知 key 静默返回空结果，同一接口两后端行为相悖，
// 配置错误被当成"没搜到"）。
var filterKeys = map[string]bool{"file": true, "heading": true}

// ValidateFilter 检索过滤字段校验：白名单外的 key 直接报错（fail-fast）。
// 两后端共用（Local/向量后端同一契约）——导出面供 rag/milvus 子包复用，
// 判定面单源（子包不另写一份白名单）。
func ValidateFilter(filter Filter) error {
	for k := range filter {
		if !filterKeys[k] {
			return fmt.Errorf("rag: 不支持的过滤字段 %q（可用: file, heading）", k)
		}
	}
	return nil
}

// KnowledgeService 知识检索服务接口。
// 模式一：预检索（确定性）—— workflow 阶段在拼 prompt 前取 topK 片段。
// 模式二：工具化（自主）—— 包成 search_knowledge(query) 挂进 ReAct agent。
type KnowledgeService interface {
	Retrieve(ctx context.Context, query string, topK int, filter Filter) ([]Chunk, error)
	AsTool() tool.BaseTool
}

// Noop 空实现：Retrieve 返回空、AsTool 返回 nil。
type Noop struct{}

// NewNoop 创建空实现。
func NewNoop() *Noop { return &Noop{} }

func (n *Noop) Retrieve(_ context.Context, _ string, _ int, _ Filter) ([]Chunk, error) {
	return nil, nil
}

func (n *Noop) AsTool() tool.BaseTool { return nil }

var _ KnowledgeService = (*Noop)(nil)

// buildAsTool 构建 search_knowledge eino 工具（Local 与向量后端共用——后者在
// rag/milvus 子包内经 NewSearchTool 复用本实现，工具文案与 schema 单源）。
// 返回 error 通道：InferTool 对入参结构体的反射可以因字段/标签问题失败
// （下游包装层改了结构体即触发），panic 只留给 AsTool 接口路径——见其 doc。
func buildAsTool(svc KnowledgeService) (tool.BaseTool, error) {
	t, err := utils.InferTool("search_knowledge",
		"检索团队知识库（编码规范/部署约定/历史评审结论/安全清单）。返回最相关的知识片段及出处。",
		func(ctx context.Context, in *searchIn) (*searchOut, error) {
			// 透传 eino 工具调用 ctx：上层取消/超时要能中断检索
			chunks, err := svc.Retrieve(ctx, in.Query, DefaultTopK, nil)
			if err != nil {
				return &searchOut{Error: err.Error()}, nil
			}
			var b strings.Builder
			for _, c := range chunks {
				content := c.Content
				content = textutil.TruncNote(content, toolSnippetRunes, "截断")
				heading := c.Metadata["heading"]
				file := c.Metadata["file"]
				if heading != "" {
					fmt.Fprintf(&b, "【%s > %s】%s\n\n", file, heading, content)
				} else {
					fmt.Fprintf(&b, "【%s】%s\n\n", file, content)
				}
			}
			return &searchOut{Results: b.String()}, nil
		})
	if err != nil {
		return nil, fmt.Errorf("rag: 构建 search_knowledge 工具失败: %w", err)
	}
	return t, nil
}

type searchIn struct {
	Query string `json:"query" jsonschema:"description=检索关键词或问题"`
}
type searchOut struct {
	Results string `json:"results,omitempty"`
	Error   string `json:"error,omitempty"`
}

// NewSearchTool 构建 search_knowledge eino 工具（v0.14.1 导出缝）：消费方包装
// KnowledgeService 后须重建工具面——包装层 AsTool 直透内层会把工具调用绕过包装
// 检索语义（bianque 知识组去重包装实需）。schema/文案与 buildAsTool 单源。
//
// 返回 error（oagentkit/rag#31）：工具 schema 并非真静态——InferTool 对入参
// 结构体的反射可因字段/标签问题失败，「静态 schema 即编程错误」是内部注释
// 不是 API 契约；导出面给 error 通道，装配期失败可被宿主接住。
//
// 向量后端（rag/milvus 子包）经本函数构建工具面——工具语义两后端一致。
func NewSearchTool(svc KnowledgeService) (tool.BaseTool, error) { return buildAsTool(svc) }
