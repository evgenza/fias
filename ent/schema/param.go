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

// Param описывает запись ГАР; XML и JSON используют генерируемую Ent-модель.
type Param struct{ ent.Schema }

func (Param) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").SchemaType(map[string]string{dialect.Postgres: "bigserial"}).StructTag(`xml:"-" json:"-"`),
		field.Int64("source_id").StructTag(`xml:"ID,attr" json:"id,string"`),
		field.String("source").SchemaType(map[string]string{dialect.Postgres: "text"}).Default("").StructTag(`xml:"-" json:"source,omitempty"`),
		field.Int64("object_id").StructTag(`xml:"OBJECTID,attr" json:"object_id,string"`),
		field.Int64("change_id").StructTag(`xml:"CHANGEID,attr" json:"change_id,omitempty,string"`).Optional().Nillable(),
		field.Int64("change_id_end").StructTag(`xml:"CHANGEIDEND,attr" json:"change_id_end,string"`),
		field.Int64("type_id").StructTag(`xml:"TYPEID,attr" json:"type_id,string"`),
		field.String("value").SchemaType(map[string]string{dialect.Postgres: "text"}).StructTag(`xml:"VALUE,attr" json:"value"`),
		field.String("update_date").GoType(gartype.Date("")).SchemaType(map[string]string{dialect.Postgres: "date"}).StructTag(`xml:"UPDATEDATE,attr" json:"update_date"`),
		field.String("start_date").GoType(gartype.Date("")).SchemaType(map[string]string{dialect.Postgres: "date"}).StructTag(`xml:"STARTDATE,attr" json:"start_date"`),
		field.String("end_date").GoType(gartype.Date("")).SchemaType(map[string]string{dialect.Postgres: "date"}).StructTag(`xml:"ENDDATE,attr" json:"end_date"`),
	}
}

func (Param) Annotations() []schema.Annotation {
	return []schema.Annotation{GARFormat{Kind: "AS_PARAM"}, entsql.Annotation{Table: "param"}}
}

func (Param) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("object_id", "type_id").StorageKey("param_object_type_idx"),
		index.Fields("source", "source_id").Unique().StorageKey("param_source_id_idx"),
	}
}
