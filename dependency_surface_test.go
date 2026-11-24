package rag

import (
	"os/exec"
	"strings"
	"testing"
)

// 本文件是「依赖面」判据（oagentkit/rag#156）：向量后端挪到 rag/milvus 子包
// 之后，**本模块除该子包外的每个包**都不得把向量 sdk（或其重传递依赖）拖进
// 依赖闭包——否则「import 根包不链向量 sdk」这条性质会在某次重构中被无声
// 打破，而没有任何判据会红。
//
// 判据走 `go list -deps`（依赖闭包的权威口径），不是源码文本扫描：文本扫描
// 只能看见**直接** import，看不见传递链（sdk 的重传递依赖正是从传递链进来的）。

// vectorSDKPrefix 向量 sdk 的 import 路径前缀。
const vectorSDKPrefix = "github.com/milvus-io/milvus-sdk-go"

// vectorSDKHeavyDeps 向量 sdk 的重传递依赖（矩阵最重传递链）：根包不得链。
var vectorSDKHeavyDeps = []string{
	"github.com/cockroachdb/errors",
	"github.com/getsentry/sentry-go",
	"github.com/gogo/protobuf",
}

// modulePathPrefix 本模块的 import 路径前缀（判据只管本模块的包）。
const modulePathPrefix = "github.com/oagentkit/rag"

// vectorBackendPkg 唯一允许链接向量 sdk 的包（向量后端子包本体）。
const vectorBackendPkg = "github.com/oagentkit/rag/milvus"

// TestModuleDependencySurface 依赖面判据（oagentkit/rag#156）：
//
//	本模块的每个包（除 rag/milvus 外）⇒ 依赖闭包里不得出现向量 sdk 及其重传递依赖。
//
// 判据跑不起来时**判红**：找不到 go 可执行文件、或 `go list` 失败，一律 t.Fatal
// ——「判不了」不等于「没问题」，静默跳过等于没有这条判据。
func TestModuleDependencySurface(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("找不到 go 可执行文件：依赖面判据跑不起来（判绿即失效）: %v", err)
	}
	// 每行格式：<包路径>|<该包的传递依赖，空格分隔>
	out, err := exec.Command(goBin, "list", "-deps", "-f", `{{.ImportPath}}|{{join .Deps " "}}`, "./...").CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps 失败（依赖面判据判不了即判红）: %v\n%s", err, out)
	}

	sawVectorBackend := false
	checked := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) != 2 {
			continue
		}
		pkg, deps := parts[0], parts[1]
		if !strings.HasPrefix(pkg, modulePathPrefix) {
			continue
		}
		if pkg == vectorBackendPkg {
			sawVectorBackend = true
			// 反向验证（对照组）：向量后端侧确实链着 sdk——否则本判据是空转的
			if !containsPkg(deps, vectorSDKPrefix) {
				t.Fatalf("向量后端子包 %s 竟未链接向量 sdk：本判据失去对照基准（要么判据写坏了，要么隔离面被改成了别的形态）", vectorBackendPkg)
			}
			continue
		}
		checked++
		for _, dep := range strings.Fields(deps) {
			if strings.HasPrefix(dep, vectorSDKPrefix) {
				t.Fatalf("包 %s 的依赖闭包含向量 sdk %q——根包不链向量 sdk 的性质被打破（向量后端只该住在 %s）", pkg, dep, vectorBackendPkg)
			}
			for _, heavy := range vectorSDKHeavyDeps {
				if strings.HasPrefix(dep, heavy) {
					t.Fatalf("包 %s 的依赖闭包含向量 sdk 的重传递依赖 %q——根包不链向量 sdk 的性质被打破（向量后端只该住在 %s）", pkg, dep, vectorBackendPkg)
				}
			}
		}
	}
	if !sawVectorBackend {
		t.Fatalf("未在依赖图里看到 %s——向量后端子包消失或路径变了，判据失去对照基准", vectorBackendPkg)
	}
	if checked == 0 {
		t.Fatal("未判到任何本模块的包——判据面失效（go list 输出形态变了？）")
	}
}

// containsPkg 依赖列表里是否含指定前缀的包。
func containsPkg(deps, prefix string) bool {
	for _, d := range strings.Fields(deps) {
		if strings.HasPrefix(d, prefix) {
			return true
		}
	}
	return false
}
