package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"gar-importer/internal/gartype"
)

// NormativeDocType описывает запись ГАР; XML и JSON используют генерируемую Ent-модель.
type NormativeDocType struct{ ent.Schema }

func (NormativeDocType) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").GoType(gartype.Integer("")).SchemaType(map[string]string{dialect.Postgres: "numeric"}).StructTag(`xml:"ID,attr" json:"id"`).Immutable().Annotations(entsql.Annotation{Incremental: new(false)}),
		field.String("name").SchemaType(map[string]string{dialect.Postgres: "text"}).StructTag(`xml:"NAME,attr" json:"name"`),
		field.String("start_date").GoType(gartype.Date("")).SchemaType(map[string]string{dialect.Postgres: "date"}).StructTag(`xml:"STARTDATE,attr" json:"start_date"`),
		field.String("end_date").GoType(gartype.Date("")).SchemaType(map[string]string{dialect.Postgres: "date"}).StructTag(`xml:"ENDDATE,attr" json:"end_date"`),
	}
}

func (NormativeDocType) Annotations() []schema.Annotation {
	return []schema.Annotation{GARFormat{Kind: "AS_NORMATIVE_DOCS_TYPES"}, entsql.Annotation{Table: "normative_doc_types"}}
}
