package pascal

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"agentworld/internal/db"
	"agentworld/internal/llm"
	"agentworld/internal/models"
	"gorm.io/gorm"
)

// World 是 Pascal World 的入口。它持有一个真实工程、一个 Agent、一个 Observatory，
// 并提供 HTTP 接口运行 Smoke Test 与查看轨迹。
//
// 设计边界（与 Freeze 一致）：
//   - FPC 是物理规律（真实编译/测试），不模拟。
//   - Agent 通过 M8 Context Runtime 组装 Context，再调 LLM 决定工具；
//     不是“LLM 直接生成 Pascal”。
//   - 失败经验写入真实 Memory（db.AddMemory），下一轮由 Retriever 取回。
type World struct {
	Proj  *PascalProject
	Agent *Agent
	gdb   *gorm.DB
}

// NewWorld 构造 Pascal World。root 为工程根（含 hotelutils/ 与 hotelutils.initial/）。
func NewWorld(root string) (*World, error) {
	proj := &PascalProject{
		ID:       "hotelutils",
		Name:     "HotelUtils",
		RootPath: filepath.Join(root, "hotelutils"),
		Compiler: "fpc",
		Language: "FreePascal",
	}
	gdb, err := db.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		return nil, err
	}
	client := llm.New(os.Getenv("LLM_BASE_URL"), os.Getenv("LLM_API_KEY"), os.Getenv("LLM_MODEL"))
	ag := NewAgent(1, "PascalDev", proj, gdb, client)
	return &World{Proj: proj, Agent: ag, gdb: gdb}, nil
}

// ClearMemory 清空该 Agent 的全部经验，用于 Cold 实验基线。
func (w *World) ClearMemory() error {
	return w.gdb.Where("agent_id = ?", w.Agent.ID).Delete(&models.Memory{}).Error
}

// SmokeTest 跑 1 Agent × 5 Issues，返回每条 Issue 的指标。
// 若设置了 PASCAL_ONLY 环境变量（如 #001），则只跑该 Issue，便于单独验证。
func (w *World) SmokeTest() ([]*SmokeRecord, error) {
	only := os.Getenv("PASCAL_ONLY")
	records := make([]*SmokeRecord, 0, len(Issues))
	for _, it := range Issues {
		if only != "" && it.ID != only {
			continue
		}
		rec, err := w.Agent.RunIssue(it)
		if err != nil {
			return records, fmt.Errorf("issue %s: %w", it.ID, err)
		}
		records = append(records, rec)
	}
	return records, nil
}

// ColdWarmTest 是 Pascal World 的 Experiment 1：
//   - Cold 阶段：每 Issue 前清空 Memory（agent 无历史经验）。
//   - Warm 阶段：不清空，复用 Cold 阶段累积的 Memory 再跑一遍。
// 返回 Cold / Warm 两段指标，供比较 Think / Compile Failure / Token / Success。
func (w *World) ColdWarmTest() (map[string]interface{}, error) {
	cold, err := w.runPhase(true)
	if err != nil {
		return nil, err
	}
	// Warm 阶段复用 Cold 已写入的 Memory（不清空）
	warm, err := w.runPhase(false)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"experiment": "Pascal World Cold vs Warm",
		"agent":      w.Agent.Name,
		"issues":     len(Issues),
		"cold":       cold,
		"warm":       warm,
	}, nil
}

func (w *World) runPhase(cold bool) ([]*SmokeRecord, error) {
	records := make([]*SmokeRecord, 0, len(Issues))
	for _, it := range Issues {
		if cold {
			if err := w.ClearMemory(); err != nil {
				return records, fmt.Errorf("clear memory: %w", err)
			}
		}
		rec, err := w.Agent.RunIssue(it)
		if err != nil {
			return records, fmt.Errorf("issue %s: %w", it.ID, err)
		}
		records = append(records, rec)
	}
	return records, nil
}

// ABCExperiment 是 Experience → Behavior 的核心实验（单一变量）。
// 三组共用同一个 Agent / 同一批 Issues / 同一个 FPC / 同一个 LLM /
// 同一个 Context Budget / 同一个 Retriever / 同一个工具集。
// 唯一变化：经验如何被表示。
//
//	group "A" = No Memory      : 每 Issue 前清空 Memory（agent 无任何历史）
//	group "B" = Raw Memory     : 不清空，写入自由文本经验（默认形态）
//	group "C" = Operational    : 不清空，写入结构化 Problem/Action/Failure/Cause/Resolution
func (w *World) ABCExperiment(group string) (map[string]interface{}, error) {
	mode := MemRaw
	clearEach := false
	switch group {
	case "A":
		clearEach = true // No Memory
	case "B":
		mode = MemRaw // Raw Memory（默认）
	case "C":
		mode = MemOperational // Operational Memory
	default:
		return nil, fmt.Errorf("unknown group %q (want A/B/C)", group)
	}
	w.Agent.SetMemMode(mode)

	records := make([]*SmokeRecord, 0, len(Issues))
	for _, it := range Issues {
		if clearEach {
			if err := w.ClearMemory(); err != nil {
				return nil, fmt.Errorf("clear memory: %w", err)
			}
		}
		rec, err := w.Agent.RunIssue(it)
		if err != nil {
			// 单 Issue 失败不中止整组：记录错误后继续，保证 JSON 可产出。
			// （高峰时段 LLM 偶发超时不应让已完成的 Issues 白跑。）
			rec = &SmokeRecord{Issue: it.ID, FinalSuccess: false, Error: err.Error()}
		}
		records = append(records, rec)
	}
	return map[string]interface{}{
		"experiment":  "Pascal World A/B/C — Experience → Behavior",
		"group":       group,
		"memory_mode": string(mode),
		"agent":       w.Agent.Name,
		"issues":      len(records),
		"records":     records,
	}, nil
}

// RegisterHandlers 暴露 HTTP 接口：
//   GET  /api/run    触发 Smoke Test，返回 JSON 指标
//   GET  /api/events 返回最近轨迹事件
//   GET  /api/stream SSE 实时轨迹
//   GET  /api/snapshot 当前状态
func (w *World) RegisterHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/api/run", func(rw http.ResponseWriter, r *http.Request) {
		recs, err := w.SmokeTest()
		if err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			return
		}
		rw.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(rw).Encode(map[string]interface{}{
			"agent":  w.Agent.Name,
			"issues": len(recs),
			"records": recs,
		})
	})
	mux.HandleFunc("/api/coldwarm", func(rw http.ResponseWriter, r *http.Request) {
		rep, err := w.ColdWarmTest()
		if err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			return
		}
		rw.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(rw).Encode(rep)
	})
	mux.HandleFunc("/api/events", w.Agent.obs.SnapshotHandler())
	mux.HandleFunc("/api/snapshot", w.Agent.obs.SnapshotHandler())
	mux.HandleFunc("/api/stream", w.Agent.obs.SSEHandler())
}
