package db

import (
	stdctx "context"

	"agentworld/internal/context"
	"agentworld/internal/models"

	"gorm.io/gorm"
)

// DBMemoryStore M8.7 接线：把现有 Economy Memory 表适配为 context.MemoryStore。
//
// 设计边界（严格遵循审计结论，不扩展 M8）：
//   - 只实现 MemoryRetriever 当前需要的查询维度：
//     AgentID + Type(IN) + 可选 aboutAgentID + Importance desc, CreatedAt desc + Limit。
//   - 不新增 internal/memory 包、不做 embedding / keyword / reranking / vector DB。
//   - 与 gorm 解耦由 context.MemoryRow 承担，这里仅做 models.Memory → context.MemoryRow 映射。
type DBMemoryStore struct {
	d *gorm.DB
}

// NewDBMemoryStore 构造最小 MemoryStore 适配（传入已打开的 *gorm.DB）。
func NewDBMemoryStore(d *gorm.DB) *DBMemoryStore {
	return &DBMemoryStore{d: d}
}

// QueryMemories 实现 context.MemoryStore。
// aboutAgentID 非空时额外包含"关于该 Agent"的 about_agent 记忆（按内容/类型匹配）。
func (s *DBMemoryStore) QueryMemories(ctx stdctx.Context, agentID string, types []string, aboutAgentID string, limit int) ([]context.MemoryRow, error) {
	if limit <= 0 {
		limit = 20
	}
	var rows []models.Memory
	q := s.d.Where("agent_id = ?", agentID)
	if len(types) > 0 {
		q = q.Where("type IN ?", types)
	}
	if aboutAgentID != "" {
		// about_agent 记忆：类型标记 + 内容含目标 Agent 标识（宽松匹配，结构化层面足够）。
		q = q.Or("agent_id = ? AND type = ? AND content LIKE ?", agentID, "about_agent", "%"+aboutAgentID+"%")
	}
	err := q.Order("importance desc, created_at desc, id desc").Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]context.MemoryRow, 0, len(rows))
	for _, m := range rows {
		out = append(out, context.MemoryRow{
			ID:         m.ID,
			AgentID:    itoaInt64(m.AgentID),
			Type:       m.Type,
			Content:    m.Content,
			Importance: m.Importance,
			CreatedAt:  m.CreatedAt,
		})
	}
	return out, nil
}

// itoaInt64 极简整数转字符串（db 包内不引 fmt 负担）。
func itoaInt64(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
