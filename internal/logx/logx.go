// Package logx 提供轻量级日志系统：分级（Debug/Info/Warn/Error）、
// 结构化字段、Caller 定位、按天/按大小滚动、文本/JSON 双输出、stderr+文件双写。
//
// 性能与并发设计：
//   - 异步队列：日志入缓冲 channel，调用方不阻塞；
//   - 批量写：单 writer goroutine 按条数攒批 / 定时 flush，减少系统调用；
//   - 同步兜底：队列满降级同步写，不丢日志、不无限膨胀；
//   - Error 实时写：关键错误始终同步落盘；
//   - 滚动安全：按天或按大小滚动后 writer 自动切换到新文件，顺序为"开新→切dest→关旧"；
//   - 关闭安全：用 stop channel 控制 writer 退出，绝不 close 数据 channel，
//     从根本上避免 "send on closed channel" panic；Flush 幂等。
package logx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Level 日志级别。
type Level int

const (
	LevelError Level = iota
	LevelWarn
	LevelInfo
	LevelDebug
)

func (l Level) String() string {
	switch l {
	case LevelError:
		return "ERROR"
	case LevelWarn:
		return "WARN"
	case LevelDebug:
		return "DEBUG"
	default:
		return "INFO"
	}
}

// ParseLevel 解析字符串级别，默认 INFO。
func ParseLevel(s string) Level {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "DEBUG":
		return LevelDebug
	case "WARN", "WARNING":
		return LevelWarn
	case "ERROR":
		return LevelError
	default:
		return LevelInfo
	}
}

// Format 输出格式。
type Format int

const (
	FormatText Format = iota // 人类可读文本（默认）
	FormatJSON               // JSON 单行
)

// ParseFormat 解析格式，默认文本。
func ParseFormat(s string) Format {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "json":
		return FormatJSON
	default:
		return FormatText
	}
}

// F 结构化字段集：logx.I("think", F{"agent": name, "action": "post"})
type F map[string]any

const (
	queueSize  = 4096
	flushBatch = 64
	flushEvery = 50 * time.Millisecond
	maxSizeDef = 10 * 1024 * 1024 // 默认单文件 10MB 触发按大小滚动
)

// outWriter 负责把日志写入目标（stderr+文件）。滚动文件时替换其 writer，
// 避免"滚动后仍写旧文件"。
type outWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (o *outWriter) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.w == nil {
		return len(p), nil
	}
	return o.w.Write(p)
}

func (o *outWriter) setOutput(w io.Writer) {
	o.mu.Lock()
	o.w = w
	o.mu.Unlock()
}

// 全局状态。
var (
	level      atomic.Int32 // Level
	format     atomic.Int32 // Format
	maxSize    atomic.Int64 // 单文件大小阈值（字节）
	dest       *outWriter
	fileOut    *os.File // 当前日志文件
	fileDir    string
	fileMu     sync.Mutex
	queue      chan string
	stop       chan struct{}
	wg         sync.WaitGroup
	startOnce  sync.Once
	stopOnce   sync.Once
	started    atomic.Bool
	stopped    atomic.Bool
)

func init() {
	level.Store(int32(LevelInfo))
	format.Store(int32(FormatText))
	maxSize.Store(maxSizeDef)
	dest = &outWriter{w: os.Stderr}
}

// SetLevel 动态调整级别。
func SetLevel(l Level) { level.Store(int32(l)) }

// SetFormat 动态调整输出格式（text/json）。
func SetFormat(f Format) { format.Store(int32(f)) }

// SetMaxSize 设置单文件大小阈值（字节），超过自动滚动新文件。
func SetMaxSize(n int64) {
	if n > 0 {
		maxSize.Store(n)
	}
}

// Setup 初始化日志：levelStr 为日志级别，dir 为日志目录（空则只写 stderr）。
// 应在 main 最早处调用一次；不要在运行中重复调用（writer 仅首次启动）。
func Setup(levelStr, dir string) error {
	level.Store(int32(ParseLevel(levelStr)))
	if dir == "" {
		return nil
	}
	fileDir = dir
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("logx: 无法创建日志目录 %s: %w", dir, err)
	}
	if err := maybeRoll(); err != nil { // 打开今日文件并绑定输出
		return err
	}
	startWriter()
	return nil
}

// startWriter 启动后台 writer goroutine（仅首次生效）。
func startWriter() {
	startOnce.Do(func() {
		queue = make(chan string, queueSize)
		stop = make(chan struct{})
		wg.Add(1)
		started.Store(true)
		go writeLoop()
	})
}

// Flush 排空队列并同步写出，随后可安全退出。幂等；可重复调用。
func Flush() {
	if !started.Load() {
		return
	}
	stopOnce.Do(func() {
		stopped.Store(true)
		close(stop) // 通知 writer 退出（不关 queue）
	})
	wg.Wait()
}

// writeLoop 后台批量写循环：按条数攒批或定时 flush。
func writeLoop() {
	defer wg.Done()
	buf := &bytes.Buffer{}
	n := 0
	flush := func() {
		if n == 0 {
			return
		}
		_ = maybeRoll()
		_, _ = dest.Write(buf.Bytes())
		buf.Reset()
		n = 0
	}
	ticker := time.NewTicker(flushEvery)
	defer ticker.Stop()
	for {
		select {
		case line := <-queue:
			buf.WriteString(line)
			n++
			if n >= flushBatch {
				flush()
			}
		case <-stop:
			for {
				select {
				case line := <-queue:
					buf.WriteString(line)
					n++
				default:
					flush()
					return
				}
			}
		case <-ticker.C:
			flush()
		}
	}
}

// syncWrite 同步写一行（Error 级 / 兜底 / 未启动时）。线程安全。
func syncWrite(line string) {
	_ = maybeRoll()
	_, _ = dest.Write([]byte(line))
}

// callerOf 获取调用 logx 的源位置（跳过 logx 包自身与公共函数帧）。
func callerOf(skip int) string {
	// skip: 0=callerOf, 1=output, 2=公共函数(Debug/Info等), 3=实际调用者
	for i := skip; i < skip+8; i++ {
		_, file, line, ok := runtime.Caller(i)
		if !ok {
			break
		}
		if strings.Contains(file, "logx") {
			continue // 跳过 logx 包内部帧
		}
		return fmt.Sprintf("%s:%d", filepath.Base(file), line)
	}
	return "?:0"
}

// formatLine 组装一行日志（文本或 JSON）。
func formatLine(l Level, msg string, fields F, caller string) string {
	ts := time.Now().Format("2006/01/02 15:04:05.000000")
	if Format(format.Load()) == FormatJSON {
		rec := map[string]any{
			"time":   ts,
			"level":  l.String(),
			"msg":    msg,
			"caller": caller,
		}
		if len(fields) > 0 {
			rec["fields"] = fields
		}
		b, err := json.Marshal(rec)
		if err != nil {
			b, _ = json.Marshal(map[string]any{"time": ts, "level": l.String(), "msg": "logx: json marshal err: " + err.Error()})
		}
		return string(b) + "\n"
	}
	fb := &strings.Builder{}
	for k, v := range fields {
		fmt.Fprintf(fb, " %s=%v", k, v)
	}
	return fmt.Sprintf("%s [%s]%s %s [%s]\n", ts, l.String(), fb.String(), msg, caller)
}

// output 记录一条日志。先过滤分级、格式化，再决定入队还是同步写。
func output(l Level, msg string, fields F) {
	if l > Level(level.Load()) {
		return
	}
	caller := callerOf(2)
	line := formatLine(l, msg, fields, caller)

	// Error 级 / 未启动 / 已停止：同步写，保证关键与尾部日志实时落盘。
	if l == LevelError || !started.Load() || stopped.Load() {
		syncWrite(line)
		return
	}
	// 异步入队；队列满降级同步写（不阻塞调用方过久 + 不丢日志）。
	select {
	case queue <- line:
	default:
		syncWrite(line)
	}
}

// maybeRoll 检查是否需要滚动（按天 或 按大小）；需要则切换输出到新文件。
// 全程持有 fileMu 保证 fileOut 同步；顺序为"开新→切dest→关旧"。
func maybeRoll() error {
	if fileDir == "" {
		return nil
	}
	fileMu.Lock()
	now := time.Now()
	today := now.Format("2006-01-02")
	sizeExceed := false
	if fileOut != nil {
		if strings.Contains(fileOut.Name(), today) {
			// 同一天：仅在超过大小阈值时按大小滚动
			if st, err := fileOut.Stat(); err == nil && st.Size() >= maxSize.Load() {
				sizeExceed = true
			} else {
				fileMu.Unlock()
				return nil // 无需滚动
			}
		}
		// 日期变化 → 按天滚动
	}
	// 需要滚动（或首次打开）
	base := "agentworld-" + today
	name := filepath.Join(fileDir, base+".log")
	if sizeExceed {
		// 按大小滚动：加时间戳后缀避免重名
		name = filepath.Join(fileDir, fmt.Sprintf("%s-%d.log", base, now.Unix()))
	}
	f, err := os.OpenFile(name, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		fileMu.Unlock()
		return fmt.Errorf("logx: 打开日志文件 %s 失败: %w", name, err)
	}
	old := fileOut
	fileOut = f
	fileMu.Unlock()

	dest.setOutput(io.MultiWriter(os.Stderr, f))
	if old != nil {
		_ = old.Close()
	}
	return nil
}

// ---- 无字段的便捷函数 ----

func Debug(msg string)          { output(LevelDebug, msg, nil) }
func Info(msg string)           { output(LevelInfo, msg, nil) }
func Warn(msg string)           { output(LevelWarn, msg, nil) }
func Error(msg string)          { output(LevelError, msg, nil) }
func Debugf(f string, a ...any) { output(LevelDebug, fmt.Sprintf(f, a...), nil) }
func Infof(f string, a ...any)  { output(LevelInfo, fmt.Sprintf(f, a...), nil) }
func Warnf(f string, a ...any)  { output(LevelWarn, fmt.Sprintf(f, a...), nil) }
func Errorf(f string, a ...any) { output(LevelError, fmt.Sprintf(f, a...), nil) }

// ---- 带结构化字段的便捷函数 ----

func D(msg string, fields F) { output(LevelDebug, msg, fields) }
func I(msg string, fields F) { output(LevelInfo, msg, fields) }
func W(msg string, fields F) { output(LevelWarn, msg, fields) }
func E(msg string, fields F) { output(LevelError, msg, fields) }
