package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"gar-importer/internal/gartype"
)

// ChangeHistory описывает запись ГАР; XML и JSON используют генерируемую Ent-модель.
type ChangeHistory struct{ ent.Schema }

func (ChangeHistory) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").StorageKey("change_id").StructTag(`xml:"CHANGEID,attr" json:"change_id,string"`).Immutable().Annotations(entsql.Annotation{Incremental: new(false)}),
		field.Int64("object_id").StructTag(`xml:"OBJECTID,attr" json:"object_id,string"`),
		field.String("adr_object_id").SchemaType(map[string]string{dialect.Postgres: "uuid"}).StructTag(`xml:"ADROBJECTID,attr" json:"adr_object_id"`),
		field.Int64("opertype_id").StructTag(`xml:"OPERTYPEID,attr" json:"opertype_id,string"`),
		field.Int64("ndoc_id").StructTag(`xml:"NDOCID,attr" json:"ndoc_id,omitempty,string"`).Optional().Nillable(),
		field.String("change_date").GoType(gartype.Date("")).SchemaType(map[string]string{dialect.Postgres: "date"}).StructTag(`xml:"CHANGEDATE,attr" json:"change_date"`),
	}
}

func (ChangeHistory) Annotations() []schema.Annotation {
	return []schema.Annotation{GARFormat{Kind: "AS_CHANGE_HISTORY"}, entsql.Annotation{Table: "change_history"}}
}

func (ChangeHistory) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("object_id").StorageKey("change_history_object_id_idx"),
	}
}
