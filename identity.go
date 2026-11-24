package rag

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"unicode/utf8"
)

// 本文件是「身份键与字节守卫」的单源面：本地索引（Local）与向量后端
// （rag/milvus 子包）两路必须用**同一套**身份形态与截断规则，否则跨路归并
// （FuseRRF 按 ID）与删除/过滤表达式会静默失配（oagentkit/rag#59）。
//
// ⚠️ 为什么身份面在根包而不在向量后端子包：向量后端已挪到 rag/milvus 子包
// （oagentkit/rag#156），但 Local 路也要用同一套身份键派生 chunk ID——
// 单源必须在两路都能 import 的一侧，故留根包并导出，子包 import 根包
// （依赖方向正确：子包 → 根包，根包不反向依赖子包）。

// FileStoredMaxLen file 落库形态的字节上限（与向量后端 file 列宽同源）。
const FileStoredMaxLen = 512

// FileStoredHashLen 超限 file 截断后附加的散列后缀长度（"#" + 8 hex）。
const FileStoredHashLen = 9

// ChunkID chunk 确定性标识（file#chunk_N）：Local 与向量后端双路统一格式——
// Hybrid 的 RRF 融合按 ID 跨路归并双路共识。此前 Local 路 chunk ID 恒空，
// FuseRRF 退化为按内容做 key：同内容跨文件塌缩成一份、同路重复自增强挤占
// topK（oagentkit/rag#40）。
//
// 导出面（向量后端子包据此派生主键）：ID 形态单源，两路不得各写一份。
func ChunkID(file string, seq int) string {
	return fmt.Sprintf("%s#chunk_%d", file, seq)
}

// FileStored file 字段的落库形态：不超限原样透传；超限截到上限内并附加
// 全路径散列后缀。朴素截断会让共享长前缀的两个深路径文件 file 字段与主键同时
// 碰撞（Upsert 互相覆盖 = 静默丢数据，比超限报错更坏），散列后缀保持唯一
// （rag#43）。
//
// 也是跨路统一身份键（rag#59）：Local 侧 chunk ID、删除与过滤表达式一律由本
// 函数归一——存储形态与查询形态必须同一，否则超限路径的删除/过滤/双路融合
// 全部静默失效。限内路径为恒等，既有身份形态不变。
//
// 导出面（向量后端子包据此归一 file 列值与主键）：形态单源，两路不得各写一份。
func FileStored(file string) string {
	if len(file) <= FileStoredMaxLen {
		return file
	}
	sum := sha256.Sum256([]byte(file))
	return TruncateUTF8(file, FileStoredMaxLen-FileStoredHashLen) + "#" + hex.EncodeToString(sum[:4])
}

// TruncateUTF8 按字节上限截断，不劈开多字节 rune（截断结果仍是合法 UTF-8——
// 劈半的 rune 落库即乱码）。s 不超限时原样返回（零拷贝）。
//
// 导出面（向量后端子包据此裁剪 content/heading 列值）：截断规则单源。
func TruncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	// 截点合法 ⇔ 截点后的首字节是 rune 起始字节；否则该 rune 跨界被劈开，回退。
	for maxBytes > 0 && !utf8.RuneStart(s[maxBytes]) {
		maxBytes--
	}
	return s[:maxBytes]
}
