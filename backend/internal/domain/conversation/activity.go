package conversation

// MessageDailyActivity 表示单用户单日的消息活跃度聚合。
// 领域类型不携带 JSON/ORM 协议标签，序列化契约由 transport 层 DTO 承担。
type MessageDailyActivity struct {
	// Date 是聚合日期，格式 YYYY-MM-DD（服务端时区）。
	Date string
	// MessageCount 是当日 user/assistant 消息总数（不含 system/tool）。
	MessageCount int64
	// TokenUsage 是当日消息 token 总消耗。
	TokenUsage int64
}
