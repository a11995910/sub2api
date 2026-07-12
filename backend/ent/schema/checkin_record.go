package schema

import (
	"time"

	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// CheckinRecord 记录用户每日签到结果。
type CheckinRecord struct {
	ent.Schema
}

func (CheckinRecord) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "checkin_records"},
	}
}

func (CheckinRecord) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
	}
}

func (CheckinRecord) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("user_id"),
		field.Time("checkin_date").
			SchemaType(map[string]string{dialect.Postgres: "date"}),
		field.Float("daily_reward").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}).
			Default(0),
		field.Float("extra_reward").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}).
			Default(0),
		field.Int("month_count").
			Default(0),
		// 连续天数在签到完成时写入；旧记录保留默认值 0，由服务层按历史日期兼容补算。
		field.Int("consecutive_count").
			Default(0),
		field.JSON("extra_milestones", []int{}).
			Optional(),
		field.Time("checked_in_at").
			Default(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (CheckinRecord) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).
			Ref("checkin_records").
			Field("user_id").
			Required().
			Unique(),
	}
}

func (CheckinRecord) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "checkin_date").Unique(),
	}
}
