package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"gar-importer/internal/gartype"
)

// ObjectLevel описывает запись ГАР; XML и JSON используют генерируемую Ent-модель.
type ObjectLevel struct{ ent.Schema }

func (ObjectLevel) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").StorageKey("level").StructTag(`xml:"LEVEL,attr" json:"level"`).Immutable().Annotations(entsql.Annotation{Incremental: new(false)}),
		field.String("name").SchemaType(map[string]string{dialect.Postgres: "text"}).StructTag(`xml:"NAME,attr" json:"name"`),
		field.String("short_name").SchemaType(map[string]string{dialect.Postgres: "text"}).StorageKey("shortname").StructTag(`xml:"SHORTNAME,attr" json:"short_name,omitempty"`).Optional().Nillable(),
		field.String("update_date").GoType(gartype.Date("")).SchemaType(map[string]string{dialect.Postgres: "date"}).StructTag(`xml:"UPDATEDATE,attr" json:"update_date"`),
		field.String("start_date").GoType(gartype.Date("")).SchemaType(map[string]string{dialect.Postgres: "date"}).StructTag(`xml:"STARTDATE,attr" json:"start_date"`),
		field.String("end_date").GoType(gartype.Date("")).SchemaType(map[string]string{dialect.Postgres: "date"}).StructTag(`xml:"ENDDATE,attr" json:"end_date"`),
		field.Bool("is_active").GoType(gartype.Flag(false)).StructTag(`xml:"ISACTIVE,attr" json:"is_active"`),
	}
}

func (ObjectLevel) Annotations() []schema.Annotation {
	return []schema.Annotation{GARFormat{Kind: "AS_OBJECT_LEVELS"}, entsql.Annotation{Table: "object_levels"}}
}
