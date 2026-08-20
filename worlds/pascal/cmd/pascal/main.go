package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"agentworld/worlds/pascal"
)

// Pascal World v0.1 启动入口。
//
// 最小闭环：Issue → Perception → Intent → Context Runtime → LLM → Tool →
// FPC(real) → Test → Memory → 下一轮 Think。
//
// 环境变量（与 AgentWorld 生产运行时一致）：
//   LLM_API_KEY  必须（Pascal World 需要真实 LLM 才能闭环）
//   LLM_BASE_URL 默认 https://api.deepseek.com/v1
//   LLM_MODEL    默认 deepseek-v4-flash（注意全小写；DeepSeek API 已于
//                 2026 起停用 deepseek-chat，模型名大小写敏感，用错会 400）
//
// FPC 必须已安装并在 PATH 中（真实编译/测试，不模拟）。
func main() {
	root := os.Getenv("PASCAL_ROOT")
	if root == "" {
		// 默认指向本 World 自带的工程目录：从当前目录向上找 go.mod，
		// 再拼 worlds/pascal/projects（worlds 是仓库根的直接子目录）。
		root = defaultProjectsRoot()
	}
	root, _ = filepath.Abs(root)

	w, err := pascal.NewWorld(root)
	if err != nil {
		log.Fatalf("[pascal] init: %v", err)
	}

	// --smoke 直接跑 Smoke Test 并打印 JSON 后退出（便于 CI / 验证）。
	if hasFlag("--smoke") {
		recs, err := w.SmokeTest()
		if err != nil {
			log.Fatalf("[pascal] smoke: %v", err)
		}
		b, _ := json.MarshalIndent(map[string]interface{}{
			"agent": w.Agent.Name, "issues": len(recs), "records": recs,
		}, "", "  ")
		fmt.Println(string(b))
		return
	}

	// --coldwarm 跑 Pascal World Experiment 1（Cold vs Warm）并打印 JSON。
	if hasFlag("--coldwarm") {
		rep, err := w.ColdWarmTest()
		if err != nil {
			log.Fatalf("[pascal] coldwarm: %v", err)
		}
		b, _ := json.MarshalIndent(rep, "", "  ")
		fmt.Println(string(b))
		return
	}

	// --abc <A|B|C> 跑 Experience → Behavior 单一变量实验（A=No Memory,
	// B=Raw Memory, C=Operational Memory），打印 JSON 指标 + Replay。
	// 若同时给定 --abc-json <path>，则把 JSON 单独写入该文件，
	// 终端只保留进度日志（便于观察运行、且得到干净 JSON）。
	if v, ok := flagValue("--abc"); ok {
		rep, err := w.ABCExperiment(v)
		if err != nil {
			log.Fatalf("[pascal] abc: %v", err)
		}
		b, _ := json.MarshalIndent(rep, "", "  ")
		if jp, ok2 := flagValue("--abc-json"); ok2 {
			if werr := os.WriteFile(jp, b, 0644); werr != nil {
				log.Fatalf("[pascal] write json: %v", werr)
			}
			log.Printf("[pascal] abc group %s 完成，JSON 已写入 %s", v, jp)
			return
		}
		fmt.Println(string(b))
		return
	}

	mux := http.NewServeMux()
	w.RegisterHandlers(mux)

	addr := envOr("PASCAL_ADDR", ":8094")
	log.Printf("[pascal] v0.1 listening on %s (root=%s)", addr, root)
	log.Printf("[pascal] GET /api/run      -> Smoke Test (1 Agent x 5 Issues)")
	log.Printf("[pascal] GET /api/stream   -> SSE timeline")
	log.Printf("[pascal] GET /api/snapshot -> current state")
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("[pascal] server: %v", err)
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func hasFlag(f string) bool {
	for _, a := range os.Args[1:] {
		if a == f {
			return true
		}
	}
	return false
}

// flagValue 返回形如 "--abc C" 的紧跟参数值；不存在或缺失则返回 ("", false)。
func flagValue(f string) (string, bool) {
	args := os.Args[1:]
	for i, a := range args {
		if a == f {
			if i+1 < len(args) {
				return args[i+1], true
			}
			return "", false
		}
	}
	return "", false
}

// defaultProjectsRoot 从当前工作目录向上寻找含有 go.mod 的仓库根，
// 再拼接 worlds/pascal/projects。保证无论 exe 在何处都能定位自带工程。
func defaultProjectsRoot() string {
	wd, err := os.Getwd()
	if err != nil {
		return "worlds/pascal/projects"
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "worlds", "pascal", "projects")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return filepath.Join(wd, "worlds", "pascal", "projects")
}
