package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// UserAllowedGroup holds the edge schema definition for the user_allowed_groups relationship.
// It replaces the legacy users.allowed_groups BIGINT[] column.
type UserAllowedGroup struct {
	ent.Schema
}

func (UserAllowedGroup) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "user_allowed_groups"},
		// Composite primary key: (user_id, group_id).
		field.ID("user_id", "group_id"),
	}
}

func (UserAllowedGroup) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("user_id"),
		field.Int64("group_id"),
		field.Time("created_at").
			Immutable().
			Default(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("expires_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).
			Comment("专属分组授权过期时间；NULL 表示永久授权"),
		field.String("source").
			MaxLen(50).
			Default("manual").
			Comment("授权来源：manual / affiliate_payment_reward"),
		field.Int64("source_order_id").
			Optional().
			Nillable().
			Comment("产生该限时授权的支付订单 ID"),
		field.String("notes").
			Default("").
			SchemaType(map[string]string{dialect.Postgres: "text"}).
			Comment("授权备注"),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (UserAllowedGroup) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("user", User.Type).
			Unique().
			Required().
			Field("user_id"),
		edge.To("group", Group.Type).
			Unique().
			Required().
			Field("group_id"),
	}
}

func (UserAllowedGroup) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("group_id"),
		index.Fields("expires_at"),
		index.Fields("source", "expires_at"),
	}
}
