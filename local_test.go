package rag

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestLocalRetrieve(t *testing.T) {
	dir := t.TempDir()
	// 创建测试 markdown 文件
	_ = os.WriteFile(filepath.Join(dir, "coding-style.md"), []byte(`# 编码规范

## Go 代码风格

每个函数不超过 50 行。
错误必须显式处理，不得用 _ 忽略。
变量命名使用驼峰式。

## Python 代码风格

使用 snake_case 命名。
每行不超过 120 字符。
`), 0o644)

	_ = os.WriteFile(filepath.Join(dir, "deploy.md"), []byte(`# 部署规范

## Nacos 配置

所有服务配置必须通过 Nacos 下发。
禁止在代码中硬编码配置项。

## ArgoCD 部署

使用 ArgoCD 进行 GitOps 部署。
镜像 tag 必须使用 git commit SHA。
`), 0o644)

	local, err := NewLocal(dir)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// 检索 "Go 代码风格"
	chunks, err := local.Retrieve(ctx, "Go 代码风格", 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) == 0 {
		t.Fatal("应检索到结果")
	}
	// 结果应包含编码规范相关内容（"函数"、"命名"、"代码"等）
	found := false
	for _, c := range chunks {
		if strings.Contains(c.Content, "函数") || strings.Contains(c.Content, "命名") || strings.Contains(c.Content, "编码") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Top 结果应包含编码规范相关内容，得到: %q", chunks[0].Content[:min(80, len(chunks[0].Content))])
	}

	// 检索 "ArgoCD 部署"
	chunks2, err := local.Retrieve(ctx, "ArgoCD 部署", 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks2) == 0 {
		t.Fatal("应检索到 ArgoCD 结果")
	}
	found2 := false
	for _, c := range chunks2 {
		if strings.Contains(c.Content, "ArgoCD") || strings.Contains(c.Content, "GitOps") {
			found2 = true
			break
		}
	}
	if !found2 {
		t.Fatal("Top 结果应包含 ArgoCD 相关内容")
	}
}

func TestLocalRetrieveTopK(t *testing.T) {
	dir := t.TempDir()
	// 创建多个文件
	for i := 0; i < 10; i++ {
		_ = os.WriteFile(filepath.Join(dir, "doc"+string(rune('0'+i))+".md"),
			[]byte("# 文档 "+string(rune('0'+i))+"\n内容关于测试和规范。"), 0o644)
	}

	local, err := NewLocal(dir)
	if err != nil {
		t.Fatal(err)
	}

	chunks, _ := local.Retrieve(context.Background(), "测试规范", 3, nil)
	if len(chunks) > 3 {
		t.Fatalf("topK=3 应最多返回 3 个结果，得到 %d", len(chunks))
	}
}

func TestLocalRetrieveFilter(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "go.md"), []byte("# Go 规范\n使用 errcheck。"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "py.md"), []byte("# Python 规范\n使用 pylint。"), 0o644)

	local, _ := NewLocal(dir)

	// 过滤只看 go.md
	chunks, _ := local.Retrieve(context.Background(), "规范", 5, Filter{"file": "go.md"})
	for _, c := range chunks {
		if c.Metadata["file"] != "go.md" {
			t.Fatalf("过滤后应只返回 go.md，得到 %s", c.Metadata["file"])
		}
	}
}

func TestLocalRetrieverEmptyQuery(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "x.md"), []byte("# X"), 0o644)
	local, _ := NewLocal(dir)

	chunks, _ := local.Retrieve(context.Background(), "", 5, nil)
	if len(chunks) != 0 {
		t.Fatal("空查询应返回空结果")
	}
}

func TestLocalRescan(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "v1.md"), []byte("# 版本1\n初始内容。"), 0o644)

	local, _ := NewLocal(dir)
	chunks1, _ := local.Retrieve(context.Background(), "初始内容", 5, nil)
	if len(chunks1) == 0 {
		t.Fatal("v1 应有结果")
	}

	// 添加新文件后强制重扫描
	local.mu.Lock()
	local.scannedAt = time.Time{} // 清除扫描时间，触发重扫
	local.mu.Unlock()

	_ = os.WriteFile(filepath.Join(dir, "v2.md"), []byte("# 版本2\n新增内容关于部署。"), 0o644)
	chunks2, _ := local.Retrieve(context.Background(), "部署", 5, nil)
	found := false
	for _, c := range chunks2 {
		if strings.Contains(c.Content, "部署") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("重扫描后应能找到新文件内容")
	}
}

func TestLocalAsTool(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "test.md"), []byte("# 测试\n工具化检索。"), 0o644)
	local, _ := NewLocal(dir)

	tool := local.AsTool()
	if tool == nil {
		t.Fatal("AsTool 应返回非 nil")
	}
	info, err := tool.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "search_knowledge" {
		t.Fatalf("工具名应为 search_knowledge，得到 %s", info.Name)
	}
}

func TestNewLocalNotDir(t *testing.T) {
	f, _ := os.CreateTemp("", "test-*.md")
	_ = f.Close()
	defer func() { _ = os.Remove(f.Name()) }()

	_, err := NewLocal(f.Name())
	if err == nil {
		t.Fatal("传入文件路径应报错")
	}
}

func TestNewLocalNotExist(t *testing.T) {
	_, err := NewLocal("/nonexistent/path/that/does/not/exist")
	if err == nil {
		t.Fatal("不存在的路径应报错")
	}
}

func TestChunkMarkdownCodeBlockProtection(t *testing.T) {
	text := `# 标题

普通文本段落。

` + "```go" + `
func main() {
    // 代码块不应被拆分
    fmt.Println("hello")
}
` + "```" + `
`
	chunks := chunkMarkdown(text)
	// 代码块应完整保留在某个 chunk 中
	found := false
	for _, c := range chunks {
		if strings.Contains(c.content, "fmt.Println") && strings.Contains(c.content, "```") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("代码块应被保护不拆分")
	}
}

func TestChunkMarkdownH3Support(t *testing.T) {
	text := `# 一级

## 二级内容

这是二级的内容。

### 三级内容

这是三级的内容。`
	chunks := chunkMarkdown(text)
	headings := map[string]bool{}
	for _, c := range chunks {
		if c.heading != "" {
			headings[c.heading] = true
		}
	}
	if !headings["二级内容"] {
		t.Fatal("应支持 ## 标题")
	}
	if !headings["三级内容"] {
		t.Fatal("应支持 ### 标题")
	}
}

func TestChunkMarkdownOverlap(t *testing.T) {
	// 长文本应产生重叠块
	var long strings.Builder
	long.WriteString("# 大文档\n\n")
	for i := 0; i < 20; i++ {
		long.WriteString("这是第" + string(rune('0'+i/10)) + string(rune('0'+i%10)) + "段落，包含一些测试内容用于验证分块重叠机制是否正常工作。\n\n")
	}
	chunks := chunkMarkdown(long.String())
	if len(chunks) < 2 {
		t.Fatalf("长文本应产生多个 chunk，得到 %d", len(chunks))
	}
}

func TestLocalRescanNewContent(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "initial.md"), []byte("# 初始\n密码管理规范。"), 0o644)

	local, _ := NewLocal(dir)
	chunks1, _ := local.Retrieve(context.Background(), "密码管理", 5, nil)
	if len(chunks1) == 0 {
		t.Fatal("初始内容应有结果")
	}

	// 强制重扫描
	local.Rescan()
	chunks2, _ := local.Retrieve(context.Background(), "密码管理", 5, nil)
	if len(chunks2) == 0 {
		t.Fatal("重扫描后应保留内容")
	}
}

func TestChunkMarkdownHardCap(t *testing.T) {
	// 回归：超长单行/未闭合代码块强制成块——保护 Milvus VarChar 65535 字节上限
	base64Line := strings.Repeat("QUJD", maxChunkRunes) // 远超 maxChunkRunes 的单行
	md := "# 标题\n\n" + base64Line + "\n\n正常段落"
	// 断言用 2 倍余量：curLen 计数不含 join 换行符，逐行块会膨胀 ~20%；
	// 关键是"有界"（远小于 VarChar 65535 字节）而非精确值
	chunks := chunkMarkdown(md)
	for i, c := range chunks {
		if len([]rune(c.content)) > maxChunkRunes*2 {
			t.Fatalf("块 %d 超过硬上限: %d runes", i, len([]rune(c.content)))
		}
	}
	// 未闭合代码块内超限也要能成块
	unclosed := "```go\n" + strings.Repeat("x = 1\n", maxChunkRunes)
	for i, c := range chunkMarkdown(unclosed) {
		if len([]rune(c.content)) > maxChunkRunes*2 {
			t.Fatalf("未闭合代码块 %d 超过硬上限: %d runes", i, len([]rune(c.content)))
		}
	}
}

// TestChunkMarkdownOverlongLineInCodeBlock 回归（第六轮审计 C 级）：代码块内
// 单条超长行（粘贴 base64/超长日志恰恰常包在围栏里）按 maxChunkRunes 硬切成
// 多块——此前单行守卫只在正文路径，代码块路径 flush 出的单块越过 Milvus
// VarChar 65535 字节上限，整个文件（含正常段落）索引失败。
func TestChunkMarkdownOverlongLineInCodeBlock(t *testing.T) {
	long := strings.Repeat("a", maxChunkRunes*3+100)
	md := "# 标题\n\n```go\n" + long + "\n```\n\n正文段落"
	chunks := chunkMarkdown(md)
	if len(chunks) < 4 {
		t.Fatalf("超长行应硬切成多块，得到 %d 块", len(chunks))
	}
	for _, c := range chunks {
		if utf8.RuneCountInString(c.content) > maxChunkRunes {
			t.Fatalf("存在超限块: %d runes", utf8.RuneCountInString(c.content))
		}
	}
}

// TestChunkMarkdownHardCapAllShapes 回归（rag#85）：「单块 ≤ maxChunkRunes」
// 不变量在任何形态下成立。此前多处漏洞可产出远超上限的块，超 Milvus
// VarChar 65535 字节上限后整文件 Upsert 被拒：
//   - 围栏行豁免单行守卫："``` + 70000 字符" 整行进块 → 70003 字节单块；
//   - handleBodyLine「先追加后判阈值」：未达阈值的上文（≤599 rune 或
//     100-rune 携带尾）+ 恰好 ≤maxChunkRunes 的单行 → 4101/4001 rune 块；
//   - curLen 只计行内 rune、不计 join 换行：逐行代码块/近满 cur 追加围栏行
//     实测冲到 4799 rune。
//
// 反证样本逐一覆盖，断言 rune 与字节双上限（字节对齐 Milvus VarChar）。
func TestChunkMarkdownHardCapAllShapes(t *testing.T) {
	cases := map[string]string{
		// 形态 A（issue#85 实测 4101）：600-rune 上文 + 4000-rune 单行
		"body_4000_after_context": "# T\n\n" + strings.Repeat("B", 600) + "\n" + strings.Repeat("A", 4000) + "\n",
		// 形态 B（issue#85 实测 70003 字节）：围栏行豁免守卫，整行进块
		"overlong_fence_line": "```" + strings.Repeat("C", 70000) + "\n",
		// 携带尾(100) + 3900-rune 单行 = 4001（形态 A 同根因）
		"carry_tail_plus_3900": "## 标\n\n" + strings.Repeat("乙", 600) + "\n" + strings.Repeat("丙", 3900) + "\n",
		// 逐行代码块：curLen 不计 join 换行，强制 flush 出 ~4799 rune 块
		"code_block_newline_inflation": "```go\n" + strings.Repeat("x = 1\n", 1000),
		// 近满 cur + 短围栏行整行追加（NoLoneFence 几何形态，~4797 rune）
		"short_fence_after_full_cur": "```go\n" + strings.Repeat("x = 1\n", maxChunkRunes/5-1) + "```\n\n## 下一节\n\n正文。\n",
	}
	for name, md := range cases {
		for i, c := range chunkMarkdown(md) {
			if n := utf8.RuneCountInString(c.content); n > maxChunkRunes {
				t.Errorf("[%s] 块 %d = %d rune 超过硬上限 %d", name, i, n, maxChunkRunes)
			}
			if n := len(c.content); n > 65535 {
				t.Errorf("[%s] 块 %d = %d 字节超过 Milvus VarChar 上限 65535", name, i, n)
			}
		}
	}
}

// TestChunkMarkdownOverlongFenceLineSplitsPreservingPairing 回归（rag#85）：
// 超长围栏行按 maxChunkRunes 保序硬切成多块（每块 ≤ 上限），且开/闭状态机
// 按整行翻转一次——后续行仍按代码块语义处理，闭合围栏照常配对，围栏闭合后
// 正文恢复常规切块（切分不得破坏状态机配对）。
func TestChunkMarkdownOverlongFenceLineSplitsPreservingPairing(t *testing.T) {
	md := "```python" + strings.Repeat("C", 70000) + "\nin code line\n```\n\n## 下一节\n\n正文段落。\n"
	chunks := chunkMarkdown(md)

	total := 0
	for i, c := range chunks {
		if n := utf8.RuneCountInString(c.content); n > maxChunkRunes {
			t.Fatalf("块 %d = %d rune 超过硬上限 %d", i, n, maxChunkRunes)
		}
		total += strings.Count(c.content, "C")
	}
	if total != 70000 {
		t.Fatalf("硬切应保序保留全部内容：C 计数 %d ≠ 70000", total)
	}
	if !strings.HasPrefix(chunks[0].content, "```python") {
		t.Fatalf("首块应保留围栏前缀: %.20q", chunks[0].content)
	}
	for i, c := range chunks {
		if strings.TrimSpace(c.content) == "```" {
			t.Fatalf("块 %d 是孤立围栏垃圾块", i)
		}
	}
	found := false
	for _, c := range chunks {
		if c.heading == "下一节" && strings.Contains(c.content, "正文段落。") {
			found = true
		}
	}
	if !found {
		t.Fatalf("围栏闭合后正文应照常成块: %+v", chunks)
	}
}

// assertNoPhantomOverlapTail 通用幻影块断言：任何块不得是另一块末尾 overlap
// 窗口的逐字副本（母块已持有该文本，独立成块即 rag#41/#60 的幻影形态）。
func assertNoPhantomOverlapTail(t *testing.T, chunks []mdChunk) {
	t.Helper()
	for i, c := range chunks {
		for j, o := range chunks {
			if i == j {
				continue
			}
			other := []rune(o.content)
			if len(other) > overlapRunes && string(other[len(other)-overlapRunes:]) == c.content {
				t.Fatalf("块 %d 是块 %d 末尾 %d rune 的逐字副本（幻影块）: %q", i, j, overlapRunes, c.content)
			}
		}
	}
}

// TestChunkMarkdownNoPhantomCarryTail 回归（rag#41）：长度切块边界紧跟标题时，
// carryOverlap 留下的 100-rune 纯重叠尾不得独立成块——该尾巴是母块末尾的逐字
// 副本（此前实测：1300-rune 母块后跟一个 100-rune「幻影块」顶着上一节标题，
// Local 与 Milvus 双路索引同时被逐字重复块污染）。
func TestChunkMarkdownNoPhantomCarryTail(t *testing.T) {
	var b strings.Builder
	b.WriteString("## 甲节标题\n\n")
	b.WriteString(strings.Repeat("甲节正文内容用于触发切块。", 100)) // 1200 rune 单行，跨 600 阈值且下一行是标题
	b.WriteString("\n## 乙节标题\n\n乙节正文。\n")
	chunks := chunkMarkdown(b.String())
	if len(chunks) != 2 {
		t.Fatalf("应 2 块（甲节母块+乙节），得到 %d: %+v", len(chunks), chunks)
	}
	if chunks[0].heading != "甲节标题" || chunks[1].heading != "乙节标题" {
		t.Fatalf("标题归属错: %+v", chunks)
	}
	assertNoPhantomOverlapTail(t, chunks)

	// 对照组：边界后跟正文行（非标题）——携带尾正常作为下一块前缀保留。
	// 修掉的是「纯尾巴独立成块」，不是重叠机制本身。
	var c2 strings.Builder
	c2.WriteString("## 甲节标题\n\n")
	c2.WriteString(strings.Repeat("甲节正文内容用于触发切块。", 100))
	c2.WriteString("\n后续正文行承接携带尾。\n")
	chunks2 := chunkMarkdown(c2.String())
	if len(chunks2) != 2 {
		t.Fatalf("对照组应 2 块，得到 %d: %+v", len(chunks2), chunks2)
	}
	mother := []rune(chunks2[0].content)
	if !strings.HasPrefix(chunks2[1].content, string(mother[len(mother)-overlapRunes:])) {
		t.Fatalf("对照组下一块应以携带尾开头: %q", chunks2[1].content)
	}
}

// TestChunkMarkdownNoPhantomCarryTailBlankLine 回归（rag#60）：长度切块边界
// 与下一标题之间隔着空行（markdown 段落常态）时，携带尾不得再旋成幻影块——
// rag#41 的守卫只在边界行紧贴携带尾时生效，此前 appendLine 对空行也无条件
// 翻回 curIsCarry=false 而 cur 仍只持有携带尾，下一边界走 carryOverlap+flush
// 把母块末尾 100 rune 原样复制成独立块。
func TestChunkMarkdownNoPhantomCarryTailBlankLine(t *testing.T) {
	var b strings.Builder
	b.WriteString("## 甲节标题\n\n")
	b.WriteString(strings.Repeat("甲节正文内容用于触发切块。", 100)) // 1200 rune 单行，跨 600 阈值
	b.WriteString("\n\n## 乙节标题\n\n乙节正文。\n")             // 空行分隔（编辑器落盘常态）后接标题
	chunks := chunkMarkdown(b.String())
	if len(chunks) != 2 {
		t.Fatalf("应 2 块（甲节母块+乙节），得到 %d: %+v", len(chunks), chunks)
	}
	if chunks[0].heading != "甲节标题" || chunks[1].heading != "乙节标题" {
		t.Fatalf("标题归属错: %+v", chunks)
	}
	assertNoPhantomOverlapTail(t, chunks)
}

// TestChunkMarkdownNoPhantomCarryTailEOFNewline 回归（rag#60）：EOF 尾换行
// （几乎所有编辑器落盘形态）在携带尾之后追加一个空行——此前同样翻回
// curIsCarry，收尾 flush 把纯携带尾独立成块（100-rune 逐字幻影）。
func TestChunkMarkdownNoPhantomCarryTailEOFNewline(t *testing.T) {
	var b strings.Builder
	b.WriteString("## 甲节标题\n\n")
	b.WriteString(strings.Repeat("甲节正文内容用于触发切块。", 130)) // 1300 rune 单行，跨 600 阈值
	b.WriteString("\n")                                 // EOF 尾换行
	chunks := chunkMarkdown(b.String())
	if len(chunks) != 1 {
		t.Fatalf("应 1 块（甲节母块），得到 %d: %+v", len(chunks), chunks)
	}
	assertNoPhantomOverlapTail(t, chunks)
}

// TestChunkMarkdownNoPhantomCarryTailBlankLineHeadingEOF 回归（rag#75）：
// 「空行紧贴标题 + 文件以裸标题收尾（EOF）」形态——carry 发生在标题边界且
// cur 以空行收尾，此前 overlapTail 对 raw join 取尾时末 rune 是空行贡献的
// \n，carryTail 带尾 \n 落账，EOF flush 的 TrimSpace 内容与之不等，幻影块
// 守卫逐字比对失守（实测 chunks=2：幻影块顶着「乙节标题」、内容恰为母块
// 末尾 99 rune 逐字副本，Local 与 Milvus 双路索引同时被污染）。修复后取尾
// 对 TrimSpace 后的母块落盘文本做（携带尾恒为真后缀）+ flush 兜底比对
// TrimSpace(carryTail) 双保险。
func TestChunkMarkdownNoPhantomCarryTailBlankLineHeadingEOF(t *testing.T) {
	var b strings.Builder
	b.WriteString("## 甲节标题\n\n")
	b.WriteString(strings.Repeat("甲节正文内容用于触发切块。", 32)) // 416 rune，不跨 600 阈值（carry 发生在标题边界）
	b.WriteString("\n\n## 乙节标题")                       // 空行紧贴标题 + 文件以裸标题收尾
	chunks := chunkMarkdown(b.String())
	if len(chunks) != 1 {
		t.Fatalf("应 1 块（乙节空节不得旋出幻影块）, got %d: %+v", len(chunks), chunks)
	}
	if chunks[0].heading != "甲节标题" {
		t.Fatalf("唯一块应是甲节母块: %+v", chunks)
	}
	assertNoPhantomOverlapTail(t, chunks)

	// 对照组：末尾标题带正文——携带尾照常作为乙节块前缀保留（修的是幻影，
	// 不是携带机制本身），且携带尾必须是母块内容的真后缀（TrimSpace 后形态）。
	var c2 strings.Builder
	c2.WriteString("## 甲节标题\n\n")
	c2.WriteString(strings.Repeat("甲节正文内容用于触发切块。", 32))
	c2.WriteString("\n\n## 乙节标题\n\n乙节正文。\n")
	chunks2 := chunkMarkdown(c2.String())
	if len(chunks2) != 2 {
		t.Fatalf("对照组应 2 块（甲节母块+乙节）, got %d: %+v", len(chunks2), chunks2)
	}
	if chunks2[1].heading != "乙节标题" {
		t.Fatalf("对照组标题归属错: %+v", chunks2)
	}
	mother := []rune(chunks2[0].content)
	if !strings.HasPrefix(chunks2[1].content, string(mother[len(mother)-overlapRunes:])) {
		t.Fatalf("乙节块应以携带尾（母块真后缀）开头: %q", chunks2[1].content)
	}
	assertNoPhantomOverlapTail(t, chunks2)
}

// TestChunkMarkdownNoLoneFenceGarbageChunk 回归（rag#41 同根因副产物）：代码块
// 内 maxChunkRunes 强制 flush 后紧跟围栏闭合行，不得产出 content 仅为 "```"
// 的垃圾块（闭合围栏落在空 cur 时丢弃——围栏行无检索价值）。
func TestChunkMarkdownNoLoneFenceGarbageChunk(t *testing.T) {
	var b strings.Builder
	b.WriteString("```go\n")
	// "```go" 占 5 rune；正文行每行 5 rune，恰好填满强制 flush 阈值让 cur 清空
	for i := 0; i < maxChunkRunes/5-1; i++ {
		b.WriteString("x = 1\n")
	}
	b.WriteString("```\n\n## 下一节\n\n正文。\n")
	chunks := chunkMarkdown(b.String())
	for i, c := range chunks {
		if strings.TrimSpace(c.content) == "```" {
			t.Fatalf("块 %d 是孤立围栏垃圾块: %+v", i, c)
		}
	}
	// 围栏后的标题与正文照常成块
	found := false
	for _, c := range chunks {
		if c.heading == "下一节" && strings.Contains(c.content, "正文。") {
			found = true
		}
	}
	if !found {
		t.Fatalf("下一节内容缺失: %+v", chunks)
	}
}

// TestLocalChunkStableID 回归（rag#40）：Local 索引的 chunk 带确定性 ID
// （file#chunk_N，与 Milvus 主键同格式）——此前 ID 恒空，FuseRRF 退化为按内容
// 做 key：同内容跨文件塌缩成一份、同路重复自增强挤占 topK。
func TestLocalChunkStableID(t *testing.T) {
	dir := t.TempDir()
	body := "相同的许可证头内容，用于构造跨文件同内容 chunk。\n"
	_ = os.WriteFile(filepath.Join(dir, "a.md"), []byte("# A\n\n"+body), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "b.md"), []byte("# B\n\n"+body), 0o644)

	l, err := NewLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	chunks, err := l.Retrieve(context.Background(), "许可证头", 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 2 {
		t.Fatalf("两文件的 chunk 都应入榜（同内容不塌缩），得到 %d: %+v", len(chunks), chunks)
	}
	ids := map[string]bool{}
	for _, c := range chunks {
		if want := chunkID(c.Metadata["file"], 0); c.ID != want {
			t.Fatalf("ID=%q want %q（file=%q）", c.ID, want, c.Metadata["file"])
		}
		ids[c.ID] = true
	}
	if len(ids) != 2 {
		t.Fatalf("两文件 ID 应互异: %+v", chunks)
	}
	// FuseRRF 按 ID 融合：同内容不同 ID 不塌缩（去重语义=同源去重）
	if got := FuseRRF(5, chunks); len(got) != 2 {
		t.Fatalf("FuseRRF 不应跨文件塌缩同内容 chunk: %+v", got)
	}
}

func TestChunkPreview(t *testing.T) {
	content := "# 手册\n\nintro 段落。\n\n## 内存排查\n\n" + strings.Repeat("OOM 排查步骤。", 80) + "\n\n## 收尾\n\ndone。"
	chunks := ChunkPreview(content)
	if len(chunks) < 3 {
		t.Fatalf("chunks=%d want >=3（intro+长正文按预算至少一块+收尾）", len(chunks))
	}
	if chunks[0].Metadata["heading"] != "手册" {
		t.Fatalf("首块 heading=%q want 手册", chunks[0].Metadata["heading"])
	}
	// 标题归属：块内容出现任一块带「内存排查」标题、尾块带「收尾」
	seen := map[string]bool{}
	for _, c := range chunks {
		seen[c.Metadata["heading"]] = true
	}
	if !seen["内存排查"] || !seen["收尾"] {
		t.Fatalf("headings 缺失: %v", seen)
	}
	if chunks[1].Content == "" {
		t.Fatal("内容块为空")
	}
	// 空输入=零块
	if got := ChunkPreview(""); len(got) != 0 {
		t.Fatalf("空输入块数=%d want 0", len(got))
	}
}

func TestRetrieveScoreFilled(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "a.md"), []byte("# 手册\n\nquartzflux 独特标记词排查路径。\n"), 0o644)
	l, err := NewLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	l.Rescan()
	chunks, err := l.Retrieve(context.Background(), "quartzflux", 5, nil)
	if err != nil || len(chunks) == 0 {
		t.Fatalf("retrieve: %v chunks=%d", err, len(chunks))
	}
	if chunks[0].Score <= 0 {
		t.Fatalf("Score 未回填: %v", chunks[0].Score)
	}
}

// rescan 单文件读取失败必须告警（rag#33）：doc 承诺「部分失败告警后照常
// 换入」，此前静默跳过 = 文档从索引消失且无信号。
func TestRescanReadErrorWarns(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "a.md"), []byte("正常文档内容"), 0o644)
	locked := filepath.Join(dir, "locked.md")
	_ = os.WriteFile(locked, []byte("不可读"), 0o000) // 权限拒绝形态
	l, err := NewLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	// 不可读文件不进索引（跳过语义不变；告警面由 ragLog 输出——包级
	// Delegating logger 无法在测试内断言，此处钉住跳过+不炸）
	chunks, err := l.Retrieve(context.Background(), "不可读", 5, nil)
	if err != nil {
		t.Fatalf("部分失败应照常换入索引: %v", err)
	}
	for _, c := range chunks {
		if strings.Contains(c.Content, "不可读") {
			t.Fatalf("不可读文件不应进索引: %+v", c)
		}
	}
	// 正常文档照常入索引
	if chunks2, _ := l.Retrieve(context.Background(), "正常文档", 5, nil); len(chunks2) == 0 {
		t.Fatal("正常文档应入索引")
	}
	_ = locked
}

// TestRescanAllReadFailedKeepsOldIndex 回归（oagentkit/rag#131）：目录可
// readdir、WalkDir 全程无错，但全部 .md ReadFile 失败（fd 耗尽/批量权限
// 拒绝/FUSE 半故障）时，必须保留旧索引并进入退避——此前保留条件只认
// walkErr，空索引照常换入，知识库静默清空（总失败的处置比部分失败更差）。
func TestRescanAllReadFailedKeepsOldIndex(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "a.md")
	if err := os.WriteFile(md, []byte("# 甲\n\n全败前的内容。"), 0o644); err != nil {
		t.Fatal(err)
	}
	l, err := NewLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	// 全部 .md 读失败：目录仍可遍历，ReadFile 权限拒绝。
	if err := os.Chmod(md, 0o000); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(md, 0o644) }()

	l.Rescan()

	chunks, err := l.Retrieve(context.Background(), "全败前", 5, nil)
	if err != nil {
		t.Fatalf("全量读失败不应影响旧索引检索: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("全量读失败应保留旧索引，实际被空索引替换")
	}
	l.mu.Lock()
	indexLen := len(l.index)
	retryAt := l.retryAt
	l.mu.Unlock()
	if indexLen == 0 {
		t.Fatal("索引被清空")
	}
	if retryAt.IsZero() {
		t.Fatal("全量读失败应进入退避（rag#135）")
	}

	// 反向对照：用户清空目录（无读失败）仍应正常换入空索引。
	if err := os.Chmod(md, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(md); err != nil {
		t.Fatal(err)
	}
	l.Rescan()
	l.mu.Lock()
	indexLen = len(l.index)
	l.mu.Unlock()
	if indexLen != 0 {
		t.Fatalf("用户清空目录应换入空索引，实际保留 %d 块", indexLen)
	}
}

// flipCancelCtx 在第 after 次 Err() 调用后开始返回 context.Canceled——
// 确定性模拟"检查点之间被取消"（真实取消落在 I/O 中间，无法用 sleep 稳定
// 复现）。仅测试单 goroutine 使用。
type flipCancelCtx struct {
	context.Context
	after int
	calls int
}

func (c *flipCancelCtx) Err() error {
	c.calls++
	if c.calls > c.after {
		return context.Canceled
	}
	return nil
}

// TestRetrieveAlreadyCanceledContext 回归（oagentkit/rag#133）：入口即检查
// ctx——此前 `_ = ctx` 丢弃取消信号，同步 Rescan+全索引打分照跑完。
func TestRetrieveAlreadyCanceledContext(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "a.md"), []byte("# 甲\n\n可检索内容。"), 0o644)
	l, err := NewLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := l.Retrieve(ctx, "可检索", 5, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("已取消 ctx 应返回 context.Canceled: %v", err)
	}
}

// TestRetrieveCancelMidRescanKeepsOldIndex 回归（oagentkit/rag#133）：过期重扫
// 中途取消必须放弃换入并保留旧索引；旧索引此后仍可检索（一次性命中的取消
// 信号不得把索引换成半成品）。
func TestRetrieveCancelMidRescanKeepsOldIndex(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "a.md"), []byte("# 甲\n\n旧索引内容。"), 0o644)
	l, err := NewLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	// 强制过期，让本次 Retrieve 走自动重扫路径。
	l.mu.Lock()
	l.scannedAt = time.Time{}
	l.retryAt = time.Time{}
	l.mu.Unlock()

	// 第 1 次 Err()（Retrieve 入口）放行，第 2 次（扫描条目检查）取消。
	ctx := &flipCancelCtx{Context: context.Background(), after: 1}
	if _, err := l.Retrieve(ctx, "旧索引", 5, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("扫描中止应返回 context.Canceled: %v", err)
	}

	chunks, err := l.Retrieve(context.Background(), "旧索引", 5, nil)
	if err != nil || len(chunks) == 0 {
		t.Fatalf("取消中止不得换入半成品索引，旧索引应可检索: %v %+v", err, chunks)
	}
}

// TestRetrieveCancelDuringScoring 回归（oagentkit/rag#133）：未触发重扫时，
// 全索引打分同样要响应取消（入口第 1 次放行，topKChunks 第 2 次取消）。
func TestRetrieveCancelDuringScoring(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "a.md"), []byte("# 甲\n\n打分段内容。"), 0o644)
	l, err := NewLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := &flipCancelCtx{Context: context.Background(), after: 1}
	if _, err := l.Retrieve(ctx, "打分段", 5, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("打分遍历应响应取消: %v", err)
	}
	if chunks, err := l.Retrieve(context.Background(), "打分段", 5, nil); err != nil || len(chunks) == 0 {
		t.Fatalf("未取消时检索应正常: %v %+v", err, chunks)
	}
}

// TestRescanFailureBackoff 回归（oagentkit/rag#135）：根目录不可达的失败扫描
// 必须推进退避——持续故障下每次检索都重跑失败扫描 + 重复告警。退避窗口内
// 不自动重扫（新文件不可见），窗口到期后自动重扫并恢复。
func TestRescanFailureBackoff(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "v1.md"), []byte("# 甲\n\nalpha old content。"), 0o644)
	l, err := NewLocal(dir)
	if err != nil {
		t.Fatal(err)
	}

	// 故障形态：根目录不可达。强制过期让下一次 Retrieve 触发失败扫描。
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	l.mu.Lock()
	l.scannedAt = time.Time{}
	l.retryAt = time.Time{}
	l.mu.Unlock()

	chunks, err := l.Retrieve(context.Background(), "alpha", 5, nil)
	if err != nil || len(chunks) == 0 {
		t.Fatalf("失败扫描期间旧索引应继续服务: %v %+v", err, chunks)
	}
	l.mu.Lock()
	retryAt := l.retryAt
	l.mu.Unlock()
	if retryAt.IsZero() {
		t.Fatal("失败扫描后应设置退避窗口")
	}

	// 恢复目录并写入新文件：退避窗口内不得自动重扫（新内容不可见）。
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "v2.md"), []byte("# 乙\n\nzulu new content。"), 0o644)
	if chunks, err := l.Retrieve(context.Background(), "zulu", 5, nil); err != nil || len(chunks) != 0 {
		t.Fatalf("退避窗口内不应自动重扫: %v %+v", err, chunks)
	}

	// 退避到期：自动重扫，新内容可见、旧内容随全量重建消失。
	l.mu.Lock()
	l.retryAt = time.Now().Add(-time.Second)
	l.mu.Unlock()
	chunks, err = l.Retrieve(context.Background(), "zulu", 5, nil)
	if err != nil || len(chunks) == 0 {
		t.Fatalf("退避到期应自动重扫: %v %+v", err, chunks)
	}
	if chunks, _ := l.Retrieve(context.Background(), "alpha", 5, nil); len(chunks) != 0 {
		t.Fatalf("全量重建后旧文件内容应消失: %+v", chunks)
	}
}

// TestChunkMarkdownFenceLengthMatching 回归（oagentkit/rag#132）：围栏闭合按
// 反引号长度匹配——4 反引号包裹的 ``` 示例不再错位翻转，示例内 # 注释不被
// 当标题切块；闭合后真标题照常切块。
func TestChunkMarkdownFenceLengthMatching(t *testing.T) {
	md := "# 真标题\n\n" +
		"````markdown\n" +
		"```go\n" +
		"// 代码注释\n" +
		"# 示例里的伪标题\n" +
		"```\n" +
		"````\n\n" +
		"## 下一节\n\n正文内容。\n"
	chunks := chunkMarkdown(md)
	var all strings.Builder
	for _, c := range chunks {
		if c.heading == "示例里的伪标题" {
			t.Fatalf("4 反引号包裹内的 # 行被当成标题切块: %+v", chunks)
		}
		all.WriteString(c.content)
		all.WriteString("\n")
	}
	if !strings.Contains(all.String(), "# 示例里的伪标题") {
		t.Fatalf("伪标题应作为代码内容保留: %q", all.String())
	}
	found := false
	for _, c := range chunks {
		if c.heading == "下一节" && strings.Contains(c.content, "正文内容。") {
			found = true
		}
	}
	if !found {
		t.Fatalf("围栏闭合后真标题应照常切块: %+v", chunks)
	}
}

// TestChunkMarkdownFenceShorterLineIsContent 回归（oagentkit/rag#132）：3 反
// 引号行在 4 反引号块内是内容，不闭合也不翻转——其后的 # 行仍受代码块保护。
func TestChunkMarkdownFenceShorterLineIsContent(t *testing.T) {
	md := "````\n" +
		"```\n" +
		"# 仍是代码\n" +
		"````\n\n" +
		"## 正文节\n\n正文。\n"
	chunks := chunkMarkdown(md)
	for _, c := range chunks {
		if c.heading == "仍是代码" {
			t.Fatalf("短围栏后的 # 行仍在代码块内，不应成为标题: %+v", chunks)
		}
	}
	found := false
	for _, c := range chunks {
		if c.heading == "正文节" && strings.Contains(c.content, "正文。") {
			found = true
		}
	}
	if !found {
		t.Fatalf("4 反引号闭合后正文应照常切块: %+v", chunks)
	}
}
