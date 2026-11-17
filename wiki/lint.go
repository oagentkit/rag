package wiki

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/oagentkit/textx/textutil"
)

// FindingKind 结构检查发现类型。
type FindingKind string

const (
	// FindingBrokenLink 出链目标不存在。
	FindingBrokenLink FindingKind = "broken_link"
	// FindingOrphan 没有任何其它页面链接到它。
	FindingOrphan FindingKind = "orphan"
	// FindingDuplicate 不同 slug 的页面内容高度相似，是潜在重复。
	FindingDuplicate FindingKind = "duplicate"
	// FindingEmpty 正文为空。
	FindingEmpty FindingKind = "empty"
)

// DefaultDuplicateThreshold 重复候选的相似度阈值（字符 bigram 集合的
// Jaccard 系数，大小写/空白不敏感）。0.9 表示近乎逐字重复；宿主若需要更宽
// 的「同主题」候选，可在自己的 Compiler/Review 流程里另做语义判定。
const DefaultDuplicateThreshold = 0.9

// Finding 一条结构检查结果。Related 语义随 Kind 变化：
//   - FindingBrokenLink：缺失的出链目标 slug；
//   - FindingDuplicate：与之相似的另一个 slug；
//   - 其它：空。
type Finding struct {
	Kind    FindingKind
	Slug    string
	Related string
	Message string
}

// Lint 检查页面集合：断链、孤儿页、重复候选、空页。
//
// 规则：
//   - 断链：Links 中的目标不在集合内（空串忽略、重复去重）；
//   - 孤儿：没有任何其它页面链接到它；自环不算入链。单页库不报孤儿
//     （唯一页必然无入链，告警不可行动）；
//   - 重复候选：不同 slug 的 Title+Body 相似度 ≥ DefaultDuplicateThreshold，
//     每个候选对报一条（Slug 为字典序较小者，Related 为较大者）；
//   - 空页：Body 去空白后为空。
//
// 输入中的 nil 页跳过；同 slug 的重复条目在排序后只保留第一条（存储层
// 不应产生这种输入，这里只保证不 panic、输出确定）。空库返回 nil。返回的
// Findings 按 Kind/Slug/Related/Message 排序，可做 golden 断言。
func Lint(pages []*Page) []Finding {
	clean := lintPages(pages)
	if len(clean) == 0 {
		return nil
	}

	bySlug := make(map[string]*Page, len(clean))
	for _, p := range clean {
		bySlug[p.Slug] = p
	}
	inbound := make(map[string]bool, len(clean))
	for _, p := range clean {
		for _, target := range normalizeSet(p.Links) {
			if target != p.Slug {
				inbound[target] = true
			}
		}
	}

	var findings []Finding
	for _, p := range clean {
		if strings.TrimSpace(p.Body) == "" {
			findings = append(findings, Finding{
				Kind:    FindingEmpty,
				Slug:    p.Slug,
				Message: "空页：正文为空",
			})
		}
		for _, target := range normalizeSet(p.Links) {
			if _, ok := bySlug[target]; !ok {
				findings = append(findings, Finding{
					Kind:    FindingBrokenLink,
					Slug:    p.Slug,
					Related: target,
					Message: fmt.Sprintf("断链：目标 %q 不存在", target),
				})
			}
		}
		if len(clean) > 1 && p.Slug != "" && !inbound[p.Slug] {
			findings = append(findings, Finding{
				Kind:    FindingOrphan,
				Slug:    p.Slug,
				Message: "孤儿页：没有任何页面链接到它",
			})
		}
	}

	// 重复候选：clean 已按 slug 排序，i<j 保证 Slug 字典序较小。
	texts := make([]string, len(clean))
	for i, p := range clean {
		texts[i] = p.Title + "\n" + p.Body
	}
	for i := range clean {
		if clean[i].Slug == "" || strings.TrimSpace(texts[i]) == "" {
			continue
		}
		for j := i + 1; j < len(clean); j++ {
			if clean[j].Slug == "" || clean[i].Slug == clean[j].Slug {
				continue
			}
			if strings.TrimSpace(texts[j]) == "" {
				continue
			}
			sim := textutil.Similarity(texts[i], texts[j])
			if sim < DefaultDuplicateThreshold {
				continue
			}
			findings = append(findings, Finding{
				Kind:    FindingDuplicate,
				Slug:    clean[i].Slug,
				Related: clean[j].Slug,
				Message: fmt.Sprintf("重复候选：与 %q 相似度 %.2f", clean[j].Slug, sim),
			})
		}
	}

	slices.SortFunc(findings, func(a, b Finding) int {
		if c := cmp.Compare(a.Kind, b.Kind); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Slug, b.Slug); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Related, b.Related); c != 0 {
			return c
		}
		return cmp.Compare(a.Message, b.Message)
	})
	return findings
}

// lintPages 过滤 nil、按 slug 稳定去重，并按 Slug/Title/Body 排序——同 slug
// 重复条目保留哪一条与输入顺序无关，输出可复现。
func lintPages(pages []*Page) []*Page {
	sorted := make([]*Page, 0, len(pages))
	for _, p := range pages {
		if p != nil {
			sorted = append(sorted, p)
		}
	}
	slices.SortFunc(sorted, func(a, b *Page) int {
		if c := cmp.Compare(a.Slug, b.Slug); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Title, b.Title); c != 0 {
			return c
		}
		return cmp.Compare(a.Body, b.Body)
	})
	out := sorted[:0]
	seen := make(map[string]struct{}, len(sorted))
	for _, p := range sorted {
		if _, ok := seen[p.Slug]; ok {
			continue
		}
		seen[p.Slug] = struct{}{}
		out = append(out, p)
	}
	return out
}
