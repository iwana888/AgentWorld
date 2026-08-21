package pascal

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

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
			// 单 Issue 失败不中止整组（高峰偶发超时不该废掉其他 Issues）。
			rec = &SmokeRecord{Issue: it.ID, FinalSuccess: false, Error: err.Error()}
		}
		records = append(records, rec)
	}
	return records, nil
}

// ReliabilityDemo 是 Reliability Runtime 的“真实 Agent”演示入口。
// 它在每个正常 Issue 上注入一条 Trap（诱导 Agent 去写 test_*.pas），
// 挂上 Guard 跑真实闭环，从而演示：
//   Agent 被诱导 → write_file(test_*) → Guard DENY（执行前拦截）
//   → Agent 读 DENY 原因自行 Recovery → 改写 src/ 生产代码 → FPC PASS。
// 不修改基线 Issues，仅用副本注入 Trap，故 A/B/C / Smoke 不受影响。
func (w *World) ReliabilityDemo() ([]*SmokeRecord, error) {
	w.Agent.SetGuard(NewGuard())
	w.Agent.SetDemoInject(true) // 注入点：主动构造违规写测试动作，演示“想做就被拦”
	records := make([]*SmokeRecord, 0, len(Issues))
	for i, it := range Issues {
		log.Printf("[reliability-demo] start issue %d/%d id=%s", i+1, len(Issues), it.ID)
		// 单 issue 超时保护：避免高峰时段某次 LLM 卡死拖垮整个 Demo。
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
		done := make(chan struct{ rec *SmokeRecord; err error })
		go func() {
			r, e := w.Agent.RunIssue(it)
			done <- struct{ rec *SmokeRecord; err error }{r, e}
		}()
		var rec *SmokeRecord
		var err error
		select {
		case <-ctx.Done():
			rec = &SmokeRecord{Issue: it.ID, FinalSuccess: false, Error: "issue timeout (LLM slow in peak window)"}
			log.Printf("[reliability-demo] issue %s TIMEOUT", it.ID)
		case res := <-done:
			rec, err = res.rec, res.err
		}
		cancel()
		if err != nil {
			log.Printf("[reliability-demo] issue %s ERROR: %v", it.ID, err)
		} else {
			log.Printf("[reliability-demo] issue %s done success=%v guardEvents=%d", it.ID, rec.FinalSuccess, len(rec.GuardEvents))
		}
		records = append(records, rec)
	}
	return records, nil
}

// testNameFor 由 Issue 的 TestFiles[0] 取文件名（不含目录），
// 供 Trap 指名诱导写哪个测试文件。无 TestFiles 时退化到 "test_<id>.pas"。
func testNameFor(it Issue) string {
	if len(it.TestFiles) > 0 {
		return filepath.Base(it.TestFiles[0])
	}
	return "test_" + strings.TrimPrefix(it.ID, "#") + ".pas"
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
			rec = &SmokeRecord{Issue: it.ID, FinalSuccess: false, Error: err.Error()}
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
