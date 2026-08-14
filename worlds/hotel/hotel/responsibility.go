package hotel

// Responsibility 一个 Agent 的岗位责任区域（M8.1 核心设计）。
// 位置决定"在哪里"，岗位决定"负责什么"——两者分离。
type Responsibility struct {
	AgentID  int64
	HotelID  string
	Location string // 负责的位置（如 entrance）
	Role     string // 岗位（welcome / frontdesk / housekeeping / maintenance）
	Priority int    // 优先级（同岗位多个 Agent 时，高者优先）
}

// Resolver M8.1：责任解析器——当 Guest 进入某位置，决定哪个 Agent 应感知并处理。
// 选择逻辑（需求九）：
//   1. 找到负责该位置的 Agent
//   2. 过滤 Busy Agent（复用 Economy 的 BusyUntil）
//   3. 距离 / Priority 排序
//   4. 选择最合适的 Agent
type Resolver struct {
	space *Space
}

// NewResolver 创建责任解析器。
func NewResolver(space *Space) *Resolver {
	return &Resolver{space: space}
}

// Resolve 返回应感知并处理某位置事件的 Agent ID。
// 若没有任何可用的负责 Agent，返回 0（无人处理）。
func (r *Resolver) Resolve(locationID string, isBusy func(agentID int64) bool) (int64, *Responsibility) {
	bestID := int64(0)
	bestResp := (*Responsibility)(nil)
	for aid, resp := range r.space.Responsibilities() {
		if resp.Location != locationID {
			continue // 不负责该位置
		}
		// 过滤 Busy Agent（需求十六：复用 Economy BusyUntil）
		if isBusy != nil && isBusy(aid) {
			continue
		}
		// 距离 / Priority 排序：同优先级取距离近的；不同优先级取 Priority 高的
		if bestResp == nil || better(aid, resp, bestID, bestResp, r.space, locationID) {
			bestID = aid
			bestResp = resp
		}
	}
	return bestID, bestResp
}

// better 比较两个候选：Priority 高者优先；Priority 相同取距离近者。
func better(aid int64, resp *Responsibility, bid int64, best *Responsibility, space *Space, target string) bool {
	if resp.Priority != best.Priority {
		return resp.Priority > best.Priority
	}
	d1 := space.Distance(target, space.AgentLocation(aid))
	d2 := space.Distance(target, space.AgentLocation(bid))
	return d1 < d2
}
