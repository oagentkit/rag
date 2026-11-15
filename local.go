package rag

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/cloudwego/eino/components/tool"

	"github.com/oagentkit/textx/textutil"
	"github.com/oekit/logx"
)

// ragLog 包级组件 logger（ekit v0.31 Delegating 纪律：包归属用 component 属性
// 标识、懒解析到 logx.Default()——消费方可 SetDefault 统一接管级别/后端；
// 此前每次告警现场 NewSlogLogger 构造独立后端）。
var ragLog = logx.Delegating("oagentkit-rag")

const (
	defaultTopK      = 5
	chunkTargetRunes = 600
	rescanInterval   = 10 * time.Minute
	toolSnippetRunes = 800
	overlapRunes     = 100  // 块间重叠字符数，保持上下文连续性
	maxChunkRunes    = 4000 // 块硬上限：粘贴 base64/超长日志、未闭合代码块保护 Milvus VarChar 65535 字节
)

type indexedChunk struct {
	chunk     Chunk
	tokenFreq map[string]int
}

// Local 本地文件知识库（启动扫描 + 过期重扫）。
type Local struct {
	dir string

	mu        sync.Mutex
	index     []indexedChunk
	idf       map[string]float64 // token → smoothed IDF（rescan 预计算，检索时零 log）
	scannedAt time.Time
	// retryAt 失败退避截止时间（oagentkit/rag#135）：Rescan 失败（根不可达/
	// 全量读失败/ctx 中止）后置为 now+rescanInterval，期间自动过期判定不再
	// 重试——此前失败路径不写 scannedAt，持续故障下每次检索都重跑失败扫描、
	// 告警频率等于检索频率。成功换入清零；显式 Rescan() 不受退避约束。
	retryAt  time.Time
	scanning bool // 重扫进行中（并发过期触发的重扫收敛为单份）
}

// NewLocal 构造并立即扫描。dir 不存在时返回错误。
func NewLocal(dir string) (*Local, error) {
	fi, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("rag: %s 不是目录", dir)
	}
	l := &Local{dir: dir}
	l.Rescan()
	return l, nil
}

// Retrieve 检索 topK 片段。
// ctx 取消/超时在三个检查点生效（oagentkit/rag#133：此前注释自称"无阻塞点"
// 却同步全目录 Rescan，取消承诺落空）：入口、重扫触发点（目录遍历的条目间
// 检查）、打分遍历。中止时保留旧索引并返回 ctx.Err()。
// 重扫在锁外进行（构建新索引后原子换入）——并发检索不被文件 I/O 阻塞；
// 多 goroutine 同时判过期时重扫收敛为单份（scanning 标志让位，换入后对
// 所有人可见；此前并发各扫一遍，大目录下内存峰值按并发数放大）。
func (l *Local) Retrieve(ctx context.Context, query string, topK int, filter Filter) ([]Chunk, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateFilter(filter); err != nil {
		return nil, err
	}
	l.mu.Lock()
	if l.rescanDueLocked() {
		l.mu.Unlock()
		l.rescan(ctx)
		// 重扫因取消/超时中止：不再用旧索引继续打分，把取消信号交还调用方。
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		l.mu.Lock()
	}
	idx, idf := l.index, l.idf
	l.mu.Unlock()

	if topK <= 0 {
		topK = defaultTopK
	}
	qt := tokenize(query)
	if len(qt) == 0 || len(idx) == 0 {
		return nil, nil
	}
	return topKChunks(ctx, idx, idf, qt, topK, filter)
}

// rescanDueLocked 自动过期判定（调用方须持 l.mu）：距上次成功扫描 ≥
// rescanInterval，且不在失败退避窗口内。
func (l *Local) rescanDueLocked() bool {
	if time.Since(l.scannedAt) < rescanInterval {
		return false
	}
	return l.retryAt.IsZero() || !time.Now().Before(l.retryAt)
}

// scored 堆元素（检索路径局部类型，不外泄）。
type scored struct {
	c indexedChunk
	s float64
}

// siftDown 小顶堆下沉维护（堆顶=当前第 K 大）。
func siftDown(heap []scored, i int) {
	for {
		l, r := 2*i+1, 2*i+2
		m := i
		if l < len(heap) && heap[l].s < heap[m].s {
			m = l
		}
		if r < len(heap) && heap[r].s < heap[m].s {
			m = r
		}
		if m == i {
			return
		}
		heap[i], heap[m] = heap[m], heap[i]
		i = m
	}
}

// siftUp 小顶堆上浮维护。
func siftUp(heap []scored, i int) {
	for i > 0 {
		p := (i - 1) / 2
		if heap[i].s < heap[p].s {
			heap[i], heap[p] = heap[p], heap[i]
			i = p
		} else {
			return
		}
	}
}

// topKChunks 固定容量小顶堆选 topK：堆顶是当前第 K 大，全程 O(chunks·log K)，
// 替代"全量收集 + 全排序"（大 hits 切片分配是检索路径内存开销的大头）。
// ctx 每 256 个块检查一次（oagentkit/rag#133）：全索引遍历对大目录同样是
// 可感知的阻塞段，取消/超时要在打分面生效。
func topKChunks(ctx context.Context, idx []indexedChunk, idf map[string]float64, qt []string, topK int, filter Filter) ([]Chunk, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	heap := make([]scored, 0, topK)
	for i, ic := range idx {
		if i&0xff == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		if !matchFilter(ic.chunk.Metadata, filter) {
			continue
		}
		s := scoreTFIDF(ic.tokenFreq, qt, idf)
		if s <= 0 {
			continue
		}
		if len(heap) < topK {
			heap = append(heap, scored{ic, s})
			siftUp(heap, len(heap)-1)
		} else if s > heap[0].s {
			heap[0] = scored{ic, s}
			siftDown(heap, 0)
		}
	}
	slices.SortFunc(heap, func(a, b scored) int { return cmp.Compare(b.s, a.s) })
	out := make([]Chunk, 0, len(heap))
	for _, h := range heap {
		h.c.chunk.Score = h.s // 回填 TF-IDF 相关度（v0.14.3 修：此前恒 0，控制台/工具侧分值展示全空）
		out = append(out, h.c.chunk)
	}
	return out, nil
}

// AsTool 把检索包成 search_knowledge 工具。
//
// ⚠️ panic 契约（oagentkit/rag#31）：schema 反射构造失败时 panic（接口签名
// 无 error 通道）；需要 error 通道的装配路径用 NewSearchTool。
func (l *Local) AsTool() tool.BaseTool {
	t, err := buildAsTool(l)
	if err != nil {
		panic(err)
	}
	return t
}

// Rescan 强制重新扫描知识库目录（不等待自动过期，也不受失败退避约束）。
// 根目录不可达（卷卸载/被删）时保留旧索引并告警——静默换入空索引会让
// 知识库"消失"且无任何信号。
func (l *Local) Rescan() { l.rescan(context.Background()) }

// rescan 单次全量扫描。自动过期路径携带调用方 ctx（可取消）；显式 Rescan
// 走 Background——「强制」语义不因退避/取消跳过。
func (l *Local) rescan(ctx context.Context) {
	if _, err := os.Stat(l.dir); err != nil {
		l.markScanFailure()
		ragLog.Warn("agentkit.rag.rescan_root_missing", "dir", l.dir, logx.ErrKey, err.Error())
		return
	}
	l.mu.Lock()
	if l.scanning {
		// 已有全量扫描在途：让位（扫描完成后的原子换入对所有人可见，
		// scannedAt 已刷新——让位者读旧索引即可，收口轮）
		l.mu.Unlock()
		return
	}
	l.scanning = true
	l.mu.Unlock()

	out := l.scanMarkdown(ctx)
	if ctx.Err() != nil {
		// 取消落在遍历收尾之后：仍然不得换入（结果虽完整，语义上已超时）。
		out.aborted = true
	}

	// 原子换入：检索侧要么看到完整旧索引、要么完整新索引，不见中间态。
	l.mu.Lock()
	l.scanning = false
	switch {
	case out.aborted:
		// ctx 取消/超时：结果不完整，保留旧索引；同样进入退避，避免上层
		// 每次超时都重跑一轮注定被打断的全目录 I/O。
		l.retryAt = time.Now().Add(rescanInterval)
		l.mu.Unlock()
	case len(out.index) == 0 && (out.walkErr != nil || (out.readFailed > 0 && out.readOK == 0)):
		// 全量失败时保留旧索引——区分"用户清空目录"（WalkDir 无 err、无读
		// 失败，正常换入空索引）与"读失败"（保留旧索引）。oagentkit/rag#131：
		// 此前只认 walkErr，目录可 readdir 但全部 .md ReadFile 失败（fd 耗尽/
		// 批量权限拒绝/FUSE 半故障）时被当成用户清空，空索引照常换入，知识库
		// 静默清空；总失败的处置反而比部分失败更差。
		l.retryAt = time.Now().Add(rescanInterval)
		l.mu.Unlock()
		if out.readFailed > 0 && out.readErr != nil {
			ragLog.Warn("agentkit.rag.rescan_all_read_failed",
				"dir", l.dir, "files", out.readFailed, logx.ErrKey, out.readErr.Error())
		}
	default:
		// 部分失败（部分文件不可读/部分子树遍历失败）告警后照常换入。
		l.index, l.idf, l.scannedAt = out.index, computeIDF(len(out.index), out.df), time.Now()
		l.retryAt = time.Time{}
		l.mu.Unlock()
	}
}

// markScanFailure 记录失败退避（oagentkit/rag#135）：持续故障期间自动过期
// 判定按 rescanInterval 重试，告警频率不再等于检索频率。
func (l *Local) markScanFailure() {
	l.mu.Lock()
	l.retryAt = time.Now().Add(rescanInterval)
	l.mu.Unlock()
}

// scanOutcome 一次全量扫描的结果。空索引有三种成因，处置不同（rag#131/#135）：
// 用户清空目录（无错误，正常换入空索引）、遍历/读取错误（保留旧索引并退避）、
// ctx 取消（结果不完整，同样保留旧索引）。
type scanOutcome struct {
	index      []indexedChunk
	df         map[string]int
	walkErr    error // 首个 WalkDir 自身错误（不含 ctx 取消）
	readFailed int   // ReadFile 失败文件数
	readOK     int   // ReadFile 成功文件数（区分"全部读失败"与"读到空文件"）
	readErr    error // 首个 ReadFile 错误（聚合告警用）
	aborted    bool  // ctx 取消/超时中止
}

// scanMarkdown 遍历目录构建分块索引与文档频率表。
// WalkDir 对"已处理的错误"返回 nil 即跳过继续——但根目录 stat 通过而不可 readdir
// （权限收紧/挂载半故障）时整棵树读不到，静默换入空索引 = 知识库"消失"且无信号。
// 记录读取错误：全部读失败时保留旧索引（与根目录缺失同策略），部分失败告警后照常换入。
// ctx 在条目间检查（oagentkit/rag#133）：取消/超时中止遍历并置 aborted，结果不换入。
func (l *Local) scanMarkdown(ctx context.Context) scanOutcome {
	out := scanOutcome{df: map[string]int{}}
	_ = filepath.WalkDir(l.dir, func(path string, d os.DirEntry, err error) error {
		if cerr := ctx.Err(); cerr != nil {
			out.aborted = true
			return cerr
		}
		if err != nil {
			if out.walkErr == nil {
				out.walkErr = err
			}
			ragLog.Warn("agentkit.rag.rescan_entry_error", "path", path, logx.ErrKey, err.Error())
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			if cerr := ctx.Err(); cerr != nil {
				out.aborted = true
				return cerr
			}
			// 单文件不可读跳过不阻塞整体索引，但**必须告警**（doc 承诺
			// 「部分失败告警后照常换入」；此前静默跳过 = 该文档从索引消失
			// 且无信号，排查方向带偏到检索面——rag#33）
			out.readFailed++
			if out.readErr == nil {
				out.readErr = err
			}
			ragLog.Warn("agentkit.rag.rescan_read_error", "path", path, logx.ErrKey, err.Error())
			return nil
		}
		out.readOK++
		rel, _ := filepath.Rel(l.dir, path)
		indexMarkdownFile(rel, string(data), &out.index, out.df)
		return nil
	})
	return out
}

// indexMarkdownFile 单文件分块入索引并累计文档频率。
func indexMarkdownFile(rel, data string, index *[]indexedChunk, df map[string]int) {
	for i, c := range chunkMarkdown(data) {
		toks := tokenize(c.content)
		freq := make(map[string]int, len(toks))
		for _, t := range toks {
			freq[t]++
		}
		for t := range freq { // 文档频率顺手统计，省一次全索引遍历
			df[t]++
		}
		*index = append(*index, indexedChunk{
			chunk: Chunk{
				// 确定性 ID（rel#chunk_N，与 Milvus 主键同格式）：FuseRRF 按
				// ID 跨路归并共识；此前 ID 恒空，融合退化为按内容做 key——
				// 同内容跨文件塌缩 + 同路自增强挤占 topK（rag#40）。
				//
				// 身份键走落库形态 milvusFileStored（rag#59）：超过 milvusFileMaxLen
				// 的深路径与 Milvus 主键同一截断+散列形态派生，限内路径为恒等
				// （ID 形态不变）——此前 Local 用原始 rel 派生，超限路径双路 ID
				// 永不相等，FuseRRF 跨路共识静默失效。Metadata["file"] 仍存
				// 原始 rel（Local 无列宽限制，caller 传什么过滤什么）。
				ID:       chunkID(milvusFileStored(rel), i),
				Content:  c.content,
				Metadata: map[string]string{"file": rel, "heading": c.heading},
			},
			tokenFreq: freq, // 打分只用词频表；词元切片不再保留（省一份内存）
		})
	}
}

// computeIDF smoothed IDF 预计算（1 + log(N/df)，最小值 1，避免单 chunk 时 IDF=0
// 导致零分）。检索路径零 log——此前每 chunk×查询词都要重算一遍。
func computeIDF(n int, df map[string]int) map[string]float64 {
	nf := float64(n)
	idf := make(map[string]float64, len(df))
	for t, d := range df {
		idf[t] = 1 + logF(nf/float64(d))
	}
	return idf
}

type mdChunk struct {
	heading string
	content string
}

// chunkMarkdown 按标题和空行分段，合并到 ~chunkTargetRunes，支持代码块保护。
func chunkMarkdown(text string) []mdChunk {
	var z mdChunker
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		z.handleLine(line, i+1 < len(lines))
	}
	z.flush()
	return z.chunks
}

// mdChunker 切块状态机（行级副作用集中一处，标题/代码块/重叠互不渗透）。
type mdChunker struct {
	chunks      []mdChunk
	heading     string
	cur         []string
	curLen      int
	inCodeBlock bool
	// fenceLen 当前开围栏的反引号长度（inCodeBlock 为 true 时有效）。闭合
	// 要求反引号数 ≥ 该值（CommonMark；oagentkit/rag#132：此前任何 ``` 开头
	// 的行都无条件翻转，4 反引号包裹的 ``` 示例把配对错位，示例内的 # 注释
	// 被当标题切块）。
	fenceLen int
	// curIsCarry cur 仅含上次切块携带的重叠尾（此后未追加任何新内容）。
	// 此状态下再遇块边界（标题/超长行/闭合围栏）不得把尾巴独立成块——
	// 母块已逐字持有该文本，独立块是纯重复的「幻影块」（rag#41）。
	curIsCarry bool
	// carryTail curIsCarry 时 cur 持有的携带尾原文。空行（含 EOF 尾换行切出
	// 的末空行）不构成新内容，curIsCarry 状态必须跨过它们存活——此前空行把
	// 状态翻回 false 而 cur 仍只有尾，下一边界把纯尾再旋成逐字幻影块
	// （rag#60）。flush 再按内容比对兜底：即使状态意外失守，纯尾也不落块。
	carryTail string
}

// flush 当前累积行成块。无实质内容丢弃；cur 恰好只含携带尾也丢弃——母块已
// 完整持有该文本，独立成块只是逐字幻影（rag#41/#60）。兜底比对对 TrimSpace
// 后的 carryTail 做（rag#75 双保险）：即使取尾/状态侧意外带进尾随空白，母块
// 落盘内容的真后缀仍被识别为幻影，不因 raw 逐字比对失守而落块。
func (z *mdChunker) flush() {
	if len(z.cur) > 0 {
		content := strings.TrimSpace(strings.Join(z.cur, "\n"))
		if content != "" && content != strings.TrimSpace(z.carryTail) {
			z.chunks = append(z.chunks, mdChunk{heading: z.heading, content: content})
		}
	}
	z.cur = z.cur[:0]
	z.curLen = 0
	z.curIsCarry = false
	z.carryTail = ""
}

// carryOverlap 把 cur 末尾 overlap 带入下一块（标题切块与长度切块共用）。
// 携带后 cur 只含重叠尾、置 curIsCarry + 记录尾原文，由各块边界处的幻影块
// 守卫消费（rag#41/#60）。
func (z *mdChunker) carryOverlap() {
	tail := overlapTail(z.cur)
	z.flush()
	if tail != "" {
		z.cur = append(z.cur, tail)
		z.curLen = utf8.RuneCountInString(tail)
		z.curIsCarry = true
		z.carryTail = tail
	}
}

// dropCarryTail 丢弃纯携带尾（rag#41）：cur 的全部内容 = 母块末尾 overlap 的
// 逐字副本，母块已完整持有该文本——独立成块只是双路索引里的逐字重复块
// （检索面母块+幻影块同时挤占 topK，索引面每个边界多付一次 embedding）。
func (z *mdChunker) dropCarryTail() {
	z.cur = z.cur[:0]
	z.curLen = 0
	z.curIsCarry = false
	z.carryTail = ""
}

// handleLine 单行入块：超长行守卫（正文/围栏行/代码块共用）/ 代码块保护 /
// 标题切换 / 长度切块。more=是否还有后续行（末行到阈值只 flush，不带
// overlap——没有下一块可带入）。
func (z *mdChunker) handleLine(line string, more bool) {
	// 单行超限守卫前置：粘贴 base64/超长日志恰恰常包在代码围栏里，此前守卫
	// 只在正文路径——代码块内单条超长行直接越过 Milvus VarChar 65535 字节
	// 上限，flush 出的单块让整个文件（含正常段落）Insert 失败（第六轮审计
	// C 级）。围栏行同样受守卫（issue#85）：豁免曾让 "```+粘贴内容" 整行
	// 进块（实测 70003 字节单块），硬切时状态机按围栏动作更新一次不破坏配对。
	if utf8.RuneCountInString(line) > maxChunkRunes {
		if kind := z.fenceKindOf(line); kind != fenceNone {
			z.handleOverlongFenceLine(line, kind)
		} else {
			z.handleOverlongLine(line)
		}
		return
	}
	if kind := z.fenceKindOf(line); kind != fenceNone {
		n, _, _ := fenceRun(line)
		z.applyFence(kind, n)
		if kind == fenceClose && len(z.cur) == 0 {
			// 闭合围栏落在空 cur（代码块内 maxChunkRunes 强制 flush 刚切开）：
			// 收留它只会让紧随的块边界旋出一个 content 仅为 "```" 的垃圾块
			// （rag#41 同根因副产物）——围栏行无检索价值，丢弃。
			return
		}
		// 围栏行也受块硬上限投影守卫（issue#85）：近满 cur 整行追加会把块
		// 顶过 maxChunkRunes（curLen 不计 join 换行，行数多的块被换行顶爆）。
		if z.overHardCap(line) {
			z.flushBeforeHardSplit()
		}
		z.appendLine(line)
		return
	}
	if z.inCodeBlock {
		// 追加前先判硬上限投影（issue#85）：此前「先追加后判阈值」让
		// <maxChunkRunes 的存量加尾行合成 ~2×maxChunkRunes 的块（换行
		// 占长不计入 curLen，实测 4799 rune）。
		if z.overHardCap(line) {
			z.flushBeforeHardSplit()
		}
		z.appendLine(line)
		// 多行累积守卫：未闭合代码块 + 超长内容强制成块
		if z.curLen >= maxChunkRunes {
			z.flush()
		}
		return
	}
	if h, ok := mdHeading(line); ok {
		switch {
		case z.curIsCarry:
			// 长度切块边界紧跟标题：cur 仅含携带尾，丢弃而非旋成独立块——
			// 此前 carryOverlap 对纯尾巴取 overlapTail 返回 ""，flush 把
			// 100-rune 尾巴顶着上一节标题旋成「幻影块」（rag#41）
			z.dropCarryTail()
		case len(z.cur) > 0:
			z.carryOverlap()
		default:
			z.flush()
		}
		z.heading = h
		return
	}
	z.handleBodyLine(line, more)
}

// overHardCap 追加 line 后当前块的投影长度是否越过 maxChunkRunes 硬上限
// （issue#85）。块落盘形态是 TrimSpace(join(cur, "\n"))：curLen 只累计行内
// rune，行间 join 的换行（追加后共 len(cur) 个）同样占长——不计它则逐行块
// 膨胀越过声明上界（4000-rune 存量实测冲到 4799）。TrimSpace 只缩不涨，
// 投影式是块长的保守上界；curLen==0（cur 只含空行）时前导换行被 TrimSpace
// 吃掉，投影即 line 本身，单行守卫已保证 ≤ maxChunkRunes。
func (z *mdChunker) overHardCap(line string) bool {
	return z.curLen > 0 && z.curLen+utf8.RuneCountInString(line)+len(z.cur) > maxChunkRunes
}

// flushBeforeHardSplit 硬切/硬上限边界收块（与 handleOverlongLine 同一边界
// 语义）：纯携带尾丢弃（母块已持有，独立成块即幻影，rag#41）；实质内容
// plain flush——硬上限边界不带 overlap，对齐超长行硬切路径。
func (z *mdChunker) flushBeforeHardSplit() {
	if z.curIsCarry {
		z.dropCarryTail()
	} else if len(z.cur) > 0 {
		z.flush()
	}
}

// fenceKind 围栏行对状态机的作用。
type fenceKind int

const (
	fenceNone fenceKind = iota
	fenceOpen
	fenceClose
)

// fenceRun 解析围栏行：前导空白 + 连续反引号（≥3）+ 围栏后剩余内容。
// 前导空白容忍（与既有 TrimSpace 行为一致），返回反引号个数与剩余内容。
func fenceRun(line string) (n int, rest string, ok bool) {
	t := strings.TrimLeft(line, " \t")
	i := 0
	for i < len(t) && t[i] == '`' {
		i++
	}
	if i < 3 {
		return 0, "", false
	}
	return i, t[i:], true
}

// fenceKindOf 当前行对状态机的作用（oagentkit/rag#132）：闭合须同时满足
// 「反引号数 ≥ 开围栏长度」与「围栏后无信息串」（CommonMark 闭合围栏只允许
// 尾随空白）——4 反引号块里的 ```/```go 是内容而非闭合，不再错位翻转。
func (z *mdChunker) fenceKindOf(line string) fenceKind {
	n, rest, ok := fenceRun(line)
	if !ok {
		return fenceNone
	}
	if !z.inCodeBlock {
		return fenceOpen
	}
	if n >= z.fenceLen && strings.TrimSpace(rest) == "" {
		return fenceClose
	}
	return fenceNone
}

// applyFence 按动作更新围栏状态：开围栏记录长度，闭围栏清零。
func (z *mdChunker) applyFence(kind fenceKind, n int) {
	if kind == fenceOpen {
		z.inCodeBlock = true
		z.fenceLen = n
		return
	}
	z.inCodeBlock = false
	z.fenceLen = 0
}

// handleOverlongLine 超长行硬切（单一入口）：先收当前块，再按 maxChunkRunes
// rune 等分成多块（heading 上下文保留）。硬切丢行内格式优于截断丢内容，且
// 每块确定性 ≤ maxChunkRunes（此前正文路径截断、代码块路径不设防，两处漂移）。
func (z *mdChunker) handleOverlongLine(line string) {
	z.flushBeforeHardSplit()
	for _, seg := range textutil.SplitRunes(line, maxChunkRunes) {
		z.chunks = append(z.chunks, mdChunk{heading: z.heading, content: seg})
	}
}

// handleOverlongFenceLine 超长围栏行硬切（issue#85）：围栏行曾豁免单行守卫，
// "``` + 粘贴内容" 整行 appendLine 进块，单块可达任意长度（实测 70003 字节，
// 越过 Milvus VarChar 65535 上限，整文件 Upsert 被拒）。开/闭状态机按围栏
// 动作更新一次（按反引号长度配对，rag#132），行内容随后走 handleOverlongLine
// 同一硬切入口（SplitRunes 保序等分，每块确定性 ≤ maxChunkRunes）；超长围栏
// 行携带的大块内容有检索价值，不适用短围栏「空 cur 丢弃」规则。
func (z *mdChunker) handleOverlongFenceLine(line string, kind fenceKind) {
	n, _, _ := fenceRun(line)
	z.applyFence(kind, n)
	z.handleOverlongLine(line)
}

func (z *mdChunker) appendLine(line string) {
	z.cur = append(z.cur, line)
	z.curLen += utf8.RuneCountInString(line)
	if line != "" {
		// 追加了新内容，cur 不再是纯携带尾。空行不算新内容（零 rune，对块
		// 文本零贡献）——纯携带尾状态必须跨过它存活，否则长度边界与下一
		// 边界之间隔着空行/EOF 尾换行时，纯尾又被旋成逐字幻影块（rag#60）。
		z.curIsCarry = false
		z.carryTail = ""
	}
}

// handleBodyLine 正文行：达目标块长则切块（单行超限守卫已前置 handleLine）。
// 追加前先判块硬上限投影（issue#85）：此前「先追加后判阈值」让未达阈值的
// 上文（≤599 rune 或 100-rune 携带尾）+ 恰好 ≤maxChunkRunes 的单行合成
// >maxChunkRunes 的块（实测 4101 rune），违反 maxChunkRunes 的块硬上限声明。
func (z *mdChunker) handleBodyLine(line string, more bool) {
	if z.overHardCap(line) {
		z.flushBeforeHardSplit()
	}
	z.appendLine(line)
	if z.curLen >= chunkTargetRunes {
		if more {
			z.carryOverlap()
		} else {
			z.flush()
		}
	}
}

// overlapTail 取块文本末尾 overlapRunes 个字符（不足阈值返回 ""=不携带）。
// 对 TrimSpace 后的块文本（母块实际落盘形态）取尾（rag#75）：携带尾恒为母块
// 内容的真后缀——此前对 raw join 取尾，cur 以空行收尾时（空行紧贴标题边界）
// 末 rune 是空行贡献的 \n，carryTail 带尾 \n 落账，EOF flush 的 TrimSpace 内容
// 与之不等，幻影块守卫逐字比对失守，母块尾的逐字副本顶着下一节标题进索引。
func overlapTail(cur []string) string {
	if overlapRunes <= 0 || len(cur) == 0 {
		return ""
	}
	runes := []rune(strings.TrimSpace(strings.Join(cur, "\n")))
	if len(runes) <= overlapRunes {
		return ""
	}
	return string(runes[len(runes)-overlapRunes:])
}

func mdHeading(line string) (string, bool) {
	t := strings.TrimSpace(line)
	// 支持 # ## ### 三级标题
	for _, prefix := range []string{"### ", "## ", "# "} {
		if strings.HasPrefix(t, prefix) {
			return strings.TrimPrefix(t, prefix), true
		}
	}
	return "", false
}

// tokenize 分词：ASCII 词（小写）+ 非 ASCII 字母二元组（单字成词时补单字）。
// 二元组覆盖一切非 ASCII 字母——Han/谚文/假名/西里尔/希腊等一律成对（后邻
// 也是非 ASCII 字母时），不再按脚本取舍：oagentkit/rag#84 旧实现只认 Han，
// 谚文/假名文档索引成零词元、查询恒空，整类语言静默不可检索。
// 查询与文档共用本函数，二元组切分两侧一致，重叠即召回。
// 局限（有意为之）：非 ASCII 字母不折大小写（零拷贝切原串子串）；数字与非
// ASCII 字母不互连（各自成词）。
// 字节偏移迭代：bigram 直接切原串子串（零拷贝），ASCII 词写入即小写
// （词内只可能是 ASCII 字母/数字，小写化只影响 A-Z），避免 []rune 全量拷贝。
func tokenize(s string) []string {
	var out []string
	var word []byte
	addWord := func() {
		if len(word) > 0 {
			out = append(out, string(word))
		}
		word = word[:0]
	}
	for i, r := range s {
		switch {
		case (r < 128 && unicode.IsLetter(r)) || unicode.IsDigit(r):
			if r >= 'A' && r <= 'Z' {
				r += 'a' - 'A'
			}
			word = utf8.AppendRune(word, r)
		case r >= 128 && unicode.IsLetter(r):
			addWord()
			size := utf8.RuneLen(r)
			end := i + size
			if end < len(s) {
				if r2, size2 := utf8.DecodeRuneInString(s[end:]); r2 >= 128 && unicode.IsLetter(r2) {
					out = append(out, s[i:end+size2])
				} else {
					out = append(out, s[i:end])
				}
			} else {
				out = append(out, s[i:end])
			}
		default:
			addWord()
		}
	}
	addWord()
	return out
}

// scoreTFIDF TF-IDF 加权打分：sum(tf * idf[t]) / sqrt(queryLen)。
// idf 为 rescan 预计算表；tf>0 的 token 必在表中（该 chunk 含 t ⇒ df[t]≥1），
// 兜底 w==0 时取 1 作中性权重。
func scoreTFIDF(chunkFreq map[string]int, queryTokens []string, idf map[string]float64) float64 {
	if len(chunkFreq) == 0 || len(queryTokens) == 0 {
		return 0
	}
	var score float64
	for _, t := range queryTokens {
		tf := float64(chunkFreq[t])
		if tf == 0 {
			continue
		}
		w := idf[t]
		if w == 0 {
			w = 1
		}
		score += tf * w
	}
	if score == 0 {
		return 0
	}
	return score / sqrtF(float64(len(queryTokens)))
}

func logF(x float64) float64 {
	if x <= 1 {
		return 0
	}
	return math.Log(x)
}

// sqrtF 平方根（x<=0 时返回 1 作中性分母）。
func sqrtF(x float64) float64 {
	if x <= 0 {
		return 1
	}
	return math.Sqrt(x)
}

func matchFilter(meta map[string]string, filter Filter) bool {
	for k, v := range filter {
		if meta[k] != v {
			return false
		}
	}
	return true
}

// filterKeys 检索过滤字段白名单（包级单源：Local 与 Milvus 后端共用——此前
// 白名单只在 Milvus 侧，Local 对未知 key 静默返回空结果，同一接口两后端
// 行为相悖，配置错误被当成"没搜到"）。
var filterKeys = map[string]bool{"file": true, "heading": true}

// validateFilter 检索过滤字段校验：白名单外的 key 直接报错（fail-fast）。
func validateFilter(filter Filter) error {
	for k := range filter {
		if !filterKeys[k] {
			return fmt.Errorf("rag: 不支持的过滤字段 %q（可用: file, heading）", k)
		}
	}
	return nil
}

// ChunkPreview 切块只读预览（v0.14.2）：与索引侧 chunkMarkdown 同一状态机——消费方
// （bianque 知识文档管理面，对标 Dify/RAGFlow 分段预览）展示「这篇会切成几块、每块什么
// 内容」，预览与实际索引不分叉。返回块内容与所属标题（Metadata["heading"]，无标题块为空）。
func ChunkPreview(content string) []Chunk {
	parts := chunkMarkdown(content)
	out := make([]Chunk, 0, len(parts))
	for _, p := range parts {
		out = append(out, Chunk{Content: p.content, Metadata: map[string]string{"heading": p.heading}})
	}
	return out
}
