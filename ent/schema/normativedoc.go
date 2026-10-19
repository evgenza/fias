package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"gar-importer/internal/gartype"
)

// NormativeDoc описывает запись ГАР; XML и JSON используют генерируемую Ent-модель.
type NormativeDoc struct{ ent.Schema }

func (NormativeDoc) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").StructTag(`xml:"ID,attr" json:"id,string"`).Immutable().Annotations(entsql.Annotation{Incremental: new(false)}),
		field.String("name").SchemaType(map[string]string{dialect.Postgres: "text"}).StructTag(`xml:"NAME,attr" json:"name"`),
		field.String("doc_date").GoType(gartype.Date("")).SchemaType(map[string]string{dialect.Postgres: "date"}).StructTag(`xml:"DATE,attr" json:"doc_date"`),
		field.String("number").SchemaType(map[string]string{dialect.Postgres: "text"}).StructTag(`xml:"NUMBER,attr" json:"number"`),
		field.Int64("type").StructTag(`xml:"TYPE,attr" json:"type"`),
		field.Int64("kind").StructTag(`xml:"KIND,attr" json:"kind"`),
		field.String("update_date").GoType(gartype.Date("")).SchemaType(map[string]string{dialect.Postgres: "date"}).StructTag(`xml:"UPDATEDATE,attr" json:"update_date"`),
		field.String("org_name").SchemaType(map[string]string{dialect.Postgres: "text"}).StorageKey("orgname").StructTag(`xml:"ORGNAME,attr" json:"org_name,omitempty"`).Optional().Nillable(),
		field.String("reg_num").SchemaType(map[string]string{dialect.Postgres: "text"}).StorageKey("regnum").StructTag(`xml:"REGNUM,attr" json:"reg_num,omitempty"`).Optional().Nillable(),
		field.String("reg_date").GoType(gartype.Date("")).SchemaType(map[string]string{dialect.Postgres: "date"}).StructTag(`xml:"REGDATE,attr" json:"reg_date,omitempty"`).Optional().Nillable(),
		field.String("acc_date").GoType(gartype.Date("")).SchemaType(map[string]string{dialect.Postgres: "date"}).StructTag(`xml:"ACCDATE,attr" json:"acc_date,omitempty"`).Optional().Nillable(),
		field.String("comment").SchemaType(map[string]string{dialect.Postgres: "text"}).StructTag(`xml:"COMMENT,attr" json:"comment,omitempty"`).Optional().Nillable(),
	}
}

func (NormativeDoc) Annotations() []schema.Annotation {
	return []schema.Annotation{GARFormat{Kind: "AS_NORMATIVE_DOCS"}, entsql.Annotation{Table: "normative_docs"}}
}
