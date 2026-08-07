package models

import "time"

// Agent 数字人
type Agent struct {
	ID           int64     `json:"id" gorm:"primaryKey"`
	Name         string    `json:"name" gorm:"type:varchar(120)"`
	Avatar       string    `json:"avatar" gorm:"type:varchar(32)"` // css class，如 c1
	Bio          string    `json:"bio" gorm:"type:text"`
	Personality  string    `json:"personality" gorm:"type:text"`
	Interests    string    `json:"interests" gorm:"type:text"` // 逗号分隔
	SystemPrompt string    `json:"system_prompt" gorm:"type:text"`
	Model        string    `json:"model" gorm:"type:varchar(64)"`
	UseLLM       bool      `json:"use_llm" gorm:"default:false"` // 是否对该 Agent 启用真实 LLM（其余走 Mock，省 token）
	Goal         string    `json:"goal" gorm:"type:text"`          // 自主意图：该 Agent 当前想达成的长期目标（驱动行为，不新增 token 调用）
	Kind         string    `json:"kind" gorm:"type:varchar(16);default:ai"` // 身份：ai(自主 Agent) / human(人类) / hybrid(人机混合)
	World        string    `json:"world" gorm:"type:varchar(24);default:social"` // 所属世界：social(社交) / hotel(酒店) …，决定用哪个 Module 驱动
	Password     string    `json:"-" gorm:"type:varchar(64)"`      // 仅 human 身份用于登录；AI 留空。json 隐藏不暴露
	Status       string    `json:"status" gorm:"type:varchar(16)"` // running / paused
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`

	// 计算字段（不入库）
	Followers   int64 `json:"followers,omitempty" gorm:"-"`
	Following   int64 `json:"following,omitempty" gorm:"-"`
	PostCount   int64 `json:"post_count,omitempty" gorm:"-"`
	LikeCount   int64 `json:"like_count,omitempty" gorm:"-"`
	MemoryCount int64 `json:"memory_count,omitempty" gorm:"-"`
}

// Post 帖子
type Post struct {
	ID           int64     `json:"id" gorm:"primaryKey"`
	AgentID      int64     `json:"agent_id" gorm:"index"`
	AgentName    string    `json:"agent_name,omitempty" gorm:"-"`
	Avatar       string    `json:"avatar,omitempty" gorm:"-"`
	Content      string    `json:"content" gorm:"type:text"`
	LikeCount    int64     `json:"like_count" gorm:"default:0"`
	CommentCount int64     `json:"comment_count" gorm:"default:0"`
	CreatedAt    time.Time `json:"created_at"`

	Comments []Comment `json:"comments,omitempty" gorm:"-"`
}

// Comment 评论
type Comment struct {
	ID        int64     `json:"id" gorm:"primaryKey"`
	PostID    int64     `json:"post_id" gorm:"index"`
	AgentID   int64     `json:"agent_id" gorm:"index"`
	AgentName string    `json:"agent_name,omitempty" gorm:"-"`
	Avatar    string    `json:"avatar,omitempty" gorm:"-"`
	Content   string    `json:"content" gorm:"type:text"`
	CreatedAt time.Time `json:"created_at"`
}

// Like 点赞（幂等：同一 agent 对同一帖子只记一次）
type Like struct {
	ID      int64 `json:"id" gorm:"primaryKey"`
	PostID  int64 `json:"post_id" gorm:"uniqueIndex:uniq_like"`
	AgentID int64 `json:"agent_id" gorm:"uniqueIndex:uniq_like"`
}

// Follow 关注（幂等）
type Follow struct {
	ID            int64 `json:"id" gorm:"primaryKey"`
	AgentID       int64 `json:"agent_id" gorm:"uniqueIndex:uniq_follow"`
	TargetAgentID int64 `json:"target_agent_id" gorm:"uniqueIndex:uniq_follow;index:idx_follow_target"`
}

// Relationship 关系（friend/disagree/frequent_discuss/block），由互动自然形成，非预设。
// AgentID 对 TargetID 持有一条 type 关系；(agent_id, target_id) 唯一，重复推导幂等覆盖。
type Relationship struct {
	ID         int64     `json:"id" gorm:"primaryKey"`
	AgentID    int64     `json:"agent_id" gorm:"uniqueIndex:uniq_rel;index:idx_rel_agent"`
	TargetID   int64     `json:"target_id" gorm:"uniqueIndex:uniq_rel;index:idx_rel_target"`
	Type       string    `json:"type" gorm:"type:varchar(24)"` // friend / disagree / frequent_discuss / block
	UpdatedAt  time.Time `json:"updated_at"`
}

// Memory Agent 记忆
type Memory struct {
	ID         int64     `json:"id" gorm:"primaryKey"`
	AgentID    int64     `json:"agent_id" gorm:"index"`
	Type       string    `json:"type" gorm:"type:varchar(32)"`
	Content    string    `json:"content" gorm:"type:text"`
	Importance int       `json:"importance" gorm:"default:1"`
	CreatedAt  time.Time `json:"created_at"`
}

// AgentAction 行为记录（调试用）
type AgentAction struct {
	ID         int64     `json:"id" gorm:"primaryKey"`
	AgentID    int64     `json:"agent_id" gorm:"index"`
	AgentName  string    `json:"agent_name,omitempty" gorm:"-"`
	Avatar     string    `json:"avatar,omitempty" gorm:"-"`
	Action     string    `json:"action" gorm:"type:varchar(16)"`
	TargetType string    `json:"target_type" gorm:"type:varchar(16)"`
	TargetID   int64     `json:"target_id"`
	Input      string    `json:"input" gorm:"type:text"`
	Output     string    `json:"output" gorm:"type:text"`
	Thought    string    `json:"thought,omitempty" gorm:"type:text"`
	CreatedAt  time.Time `json:"created_at"`
}

// AgentMessage A2A 消息（Agent 间通信，Intent 驱动）。
// 状态机：pending → accepted / rejected → done。
// 注意：列名用 from_agent / to_agent，避免 SQL 保留字 from / to。
type AgentMessage struct {
	ID            int64                  `json:"id" gorm:"primaryKey"`
	From          int64                  `json:"from" gorm:"column:from_agent;index"`
	To            int64                  `json:"to" gorm:"column:to_agent;index"` // 接收方 AgentID（0=能力寻址/广播）
	Intent        string                 `json:"intent" gorm:"index;type:varchar(64)"`
	Payload       map[string]interface{} `json:"payload" gorm:"serializer:json"`
	Status        string                 `json:"status" gorm:"index;type:varchar(16);default:pending"`
	ReplyTo       int64                  `json:"reply_to" gorm:"index"`          // 回信：指向被回复的请求消息 ID
	CorrelationID string                 `json:"correlation_id" gorm:"index;type:varchar(64)"` // 业务级关联键
	CreatedAt     time.Time              `json:"created_at"`
}

// AgentCapability 一个 Agent 对外提供的能力声明（Agent Registry / 通讯录）。
// skill 用带版本的点分格式，如 "travel.recommend.v1"、"hotel.checkin.v1"。
type AgentCapability struct {
	ID          int64     `json:"id" gorm:"primaryKey"`
	AgentID     int64     `json:"agent_id" gorm:"index"`
	World       string    `json:"world"`
	Skill       string    `json:"skill" gorm:"index;type:varchar(64)"`
	Description string    `json:"description"`
	Price       float64   `json:"price"` // 可选：调用成本（0=免费）
	Load        int       `json:"load"`  // 可选：当前负载（0=空闲）
	UpdatedAt   time.Time `json:"updated_at"`
}

// HotelRoom 酒店房间（HotelModule 的世界）。
type HotelRoom struct {
	ID        int64  `json:"id" gorm:"primaryKey"`
	Number    string `json:"number" gorm:"type:varchar(16);uniqueIndex"`
	RoomType  string `json:"room_type" gorm:"type:varchar(24)"` // standard / deluxe / suite
	Price     int    `json:"price"`
	Status    string `json:"status" gorm:"type:varchar(16)"` // available / occupied / cleaning / maintenance
	Floor     int    `json:"floor"`
	UpdatedAt time.Time `json:"updated_at"`
}

// HotelBooking 酒店预订（HotelModule 的世界）。
type HotelBooking struct {
	ID         int64     `json:"id" gorm:"primaryKey"`
	AgentID    int64     `json:"agent_id"`     // 入住的 Agent
	AgentName  string    `json:"agent_name" gorm:"type:varchar(120)"`
	RoomID     int64     `json:"room_id"`
	RoomNumber string    `json:"room_number" gorm:"type:varchar(16)"`
	CheckIn    time.Time `json:"check_in"`
	CheckOut   time.Time `json:"check_out"`
	Status     string    `json:"status" gorm:"type:varchar(16)"` // active / checked_out
	CreatedAt  time.Time `json:"created_at"`
}

// HotelReview 酒店评价（HotelModule 的世界）。
type HotelReview struct {
	ID        int64     `json:"id" gorm:"primaryKey"`
	AgentID   int64     `json:"agent_id"`
	AgentName string    `json:"agent_name" gorm:"type:varchar(120)"`
	RoomID    int64     `json:"room_id"`
	Score     int       `json:"score"` // 1~5
	Comment   string    `json:"comment" gorm:"type:text"`
	CreatedAt time.Time `json:"created_at"`
}

// AgentState Agent 内在状态（M5）。让 Agent 因经历而改变：
// 经历 → 状态变化（Mood/Energy/SocialNeed/Attention 等）→ 影响未来决策。
// 状态由 Module 在事件后通过 ApplyStateDelta 更新，Runtime 只负责存取与自然衰减。
type AgentState struct {
	ID         int64     `json:"id" gorm:"primaryKey"`
	AgentID    int64     `json:"agent_id" gorm:"uniqueIndex"`
	Mood       int       `json:"mood"`        // 情绪 -100 ~ 100
	Energy     int       `json:"energy"`      // 精力 0 ~ 100
	Curiosity  int       `json:"curiosity"`   // 好奇 0 ~ 100
	SocialNeed int       `json:"social_need"` // 社交需求 0 ~ 100
	// M7：四维 Need。不满足会随时间上升，行为可满足（下降）。
	NeedSocial        int `json:"need_social"`        // 社交需求 0~100
	NeedKnowledge     int `json:"need_knowledge"`     // 求知需求 0~100
	NeedAchievement   int `json:"need_achievement"`   // 成就需求 0~100
	NeedEntertainment int `json:"need_entertainment"` // 娱乐需求 0~100
	Attention  string    `json:"attention" gorm:"type:text"`        // 当前关注主题（Module 自行编码）
	Variables  string    `json:"variables" gorm:"type:text"`        // Module 扩展字段（JSON 编码）
	UpdatedAt  time.Time `json:"updated_at"`
}

// WorldEvent 世界级事件（M6）。由 World Engine 主动产生，非 Agent 行为。
// 如：股市大涨、突降暴雨、全网热议某话题。所有 Agent 都可能感知。
type WorldEvent struct {
	ID        int64     `json:"id" gorm:"primaryKey"`
	Type      string    `json:"type" gorm:"type:varchar(24)"` // weather / market / news / environment / day
	Title     string    `json:"title" gorm:"type:varchar(200)"`
	Detail    string    `json:"detail" gorm:"type:text"`
	TargetTag string    `json:"target_tag" gorm:"type:varchar(64)"` // 影响的目标分类（tech/finance/life...），空=影响所有
	CreatedAt time.Time `json:"created_at"`
}

// WorldState 当前世界状态（M6）。World Engine 维护的虚拟时间/天气/热度。
type WorldState struct {
	ID        int64     `json:"id" gorm:"primaryKey"`
	Key       string    `json:"key" gorm:"type:varchar(32);uniqueIndex"` // world_time / weather / heat
	Value     string    `json:"value" gorm:"type:varchar(120)"`
	UpdatedAt time.Time `json:"updated_at"`
}

// AgentPlan Agent 的多步计划（M8）。从单动作升级到"目标→计划→逐步执行"。
// Steps 存 JSON 数组（动作名序列），StepIndex 是当前执行到第几步。
type AgentPlan struct {
	ID        int64     `json:"id" gorm:"primaryKey"`
	AgentID   int64     `json:"agent_id" gorm:"uniqueIndex:uniq_plan"`
	Goal      string    `json:"goal" gorm:"type:varchar(200)"`
	Steps     string    `json:"steps" gorm:"type:text"`     // JSON 数组 ["post","comment","follow"]
	StepIndex int       `json:"step_index"`                 // 当前步骤
	Status    string    `json:"status" gorm:"type:varchar(16)"` // active / done
	CreatedAt time.Time `json:"created_at"`
}

// AgentSnapshot AgentWorld 每日快照（M8.5 长期实验）。记录一天的"文明演化"指标，
// 用于生成趋势报告（Report #1）。
type AgentSnapshot struct {
	ID          int64     `json:"id" gorm:"primaryKey"`
	Date        string    `json:"date" gorm:"type:varchar(16);uniqueIndex"` // 2026-08-07
	AgentCount  int       `json:"agent_count"`
	ActionCount int64     `json:"action_count"` // 当天新增行为数
	PostCount   int64     `json:"post_count"`
	CommentCount int64    `json:"comment_count"`
	LikeCount   int64     `json:"like_count"`
	FollowCount int64     `json:"follow_count"`
	MemoryCount int64     `json:"memory_count"`
	// 关系分布
	RelFriend int64 `json:"rel_friend"`
	RelDisagree int64 `json:"rel_disagree"`
	RelFrequent int64 `json:"rel_frequent_discuss"`
	RelBlock int64 `json:"rel_block"`
	// 社区数（friend/frequent_discuss 边去重后的 Agent 聚类近似，暂以关系边数代替）
	CommunityCount int `json:"community_count"`
	// 话题数（按帖子内容去重的近似）
	TopicCount int64 `json:"topic_count"`
	// 需求分布（当日各 Agent 的 Need 均值）
	NeedSocialAvg      float64 `json:"need_social_avg"`
	NeedKnowledgeAvg   float64 `json:"need_knowledge_avg"`
	NeedAchievementAvg float64 `json:"need_achievement_avg"`
	NeedEntAvg         float64 `json:"need_entertainment_avg"`
	CreatedAt time.Time `json:"created_at"`
}
