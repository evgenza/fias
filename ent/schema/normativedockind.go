package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"gar-importer/internal/gartype"
)

// NormativeDocKind описывает запись ГАР; XML и JSON используют генерируемую Ent-модель.
type NormativeDocKind struct{ ent.Schema }

func (NormativeDocKind) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").GoType(gartype.Integer("")).SchemaType(map[string]string{dialect.Postgres: "numeric"}).StructTag(`xml:"ID,attr" json:"id"`).Immutable().Annotations(entsql.Annotation{Incremental: new(false)}),
		field.String("name").SchemaType(map[string]string{dialect.Postgres: "text"}).StructTag(`xml:"NAME,attr" json:"name"`),
	}
}

func (NormativeDocKind) Annotations() []schema.Annotation {
	return []schema.Annotation{GARFormat{Kind: "AS_NORMATIVE_DOCS_KINDS"}, entsql.Annotation{Table: "normative_doc_kinds"}}
}
