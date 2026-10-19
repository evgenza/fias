package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"gar-importer/internal/gartype"
)

// ParamType описывает запись ГАР; XML и JSON используют генерируемую Ent-модель.
type ParamType struct{ ent.Schema }

func (ParamType) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").StructTag(`xml:"ID,attr" json:"id,string"`).Immutable().Annotations(entsql.Annotation{Incremental: new(false)}),
		field.String("name").SchemaType(map[string]string{dialect.Postgres: "text"}).StructTag(`xml:"NAME,attr" json:"name"`),
		field.String("code").SchemaType(map[string]string{dialect.Postgres: "text"}).StructTag(`xml:"CODE,attr" json:"code"`),
		field.String("descr").SchemaType(map[string]string{dialect.Postgres: "text"}).StructTag(`xml:"DESC,attr" json:"descr,omitempty"`).Optional().Nillable(),
		field.String("update_date").GoType(gartype.Date("")).SchemaType(map[string]string{dialect.Postgres: "date"}).StructTag(`xml:"UPDATEDATE,attr" json:"update_date"`),
		field.String("start_date").GoType(gartype.Date("")).SchemaType(map[string]string{dialect.Postgres: "date"}).StructTag(`xml:"STARTDATE,attr" json:"start_date"`),
		field.String("end_date").GoType(gartype.Date("")).SchemaType(map[string]string{dialect.Postgres: "date"}).StructTag(`xml:"ENDDATE,attr" json:"end_date"`),
		field.Bool("is_active").GoType(gartype.Flag(false)).StructTag(`xml:"ISACTIVE,attr" json:"is_active"`),
	}
}

func (ParamType) Annotations() []schema.Annotation {
	return []schema.Annotation{GARFormat{Kind: "AS_PARAM_TYPES"}, entsql.Annotation{Table: "param_types"}}
}
