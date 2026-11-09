// Package rag 知识检索服务。
// 提供两种后端：Local（本地 TF-IDF）和 MilvusStore（向量数据库）。
// 检索算法 = 词元重叠打分（ASCII 词 + 非 ASCII 字母二元组）或向量相似度，
// 零外部向量库依赖（Local）或 Milvus 向量数据库（MilvusStore）。
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

// chunkID chunk 确定性标识（file#chunk_N）：Local 与 Milvus 双路统一格式——
// Hybrid 的 RRF 融合按 ID 跨路归并双路共识。此前 Local 路 chunk ID 恒空，
// FuseRRF 退化为按内容做 key：同内容跨文件塌缩成一份、同路重复自增强挤占
// topK（oagentkit/rag#40）。
func chunkID(file string, seq int) string {
	return fmt.Sprintf("%s#chunk_%d", file, seq)
}

// Filter 检索过滤条件（如 repo、规范文档名）。
type Filter map[string]string

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

// buildAsTool 构建 search_knowledge eino 工具（Local 和 MilvusStore 共用）。
// 返回 error 通道：InferTool 对入参结构体的反射可以因字段/标签问题失败
// （下游包装层改了结构体即触发），panic 只留给 AsTool 接口路径——见其 doc。
func buildAsTool(svc KnowledgeService) (tool.BaseTool, error) {
	t, err := utils.InferTool("search_knowledge",
		"检索团队知识库（编码规范/部署约定/历史评审结论/安全清单）。返回最相关的知识片段及出处。",
		func(ctx context.Context, in *searchIn) (*searchOut, error) {
			// 透传 eino 工具调用 ctx：上层取消/超时要能中断检索
			chunks, err := svc.Retrieve(ctx, in.Query, defaultTopK, nil)
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
func NewSearchTool(svc KnowledgeService) (tool.BaseTool, error) { return buildAsTool(svc) }
