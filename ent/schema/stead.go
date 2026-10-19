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

// Stead описывает запись ГАР; XML и JSON используют генерируемую Ent-модель.
type Stead struct{ ent.Schema }

func (Stead) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").GoType(gartype.Integer("")).SchemaType(map[string]string{dialect.Postgres: "numeric(19,0)"}).StructTag(`xml:"ID,attr" json:"id"`).Immutable().Annotations(entsql.Annotation{Incremental: new(false)}),
		field.String("object_id").GoType(gartype.Integer("")).SchemaType(map[string]string{dialect.Postgres: "numeric(19,0)"}).StructTag(`xml:"OBJECTID,attr" json:"object_id"`),
		field.String("object_guid").SchemaType(map[string]string{dialect.Postgres: "uuid"}).StructTag(`xml:"OBJECTGUID,attr" json:"object_guid"`),
		field.String("change_id").GoType(gartype.Integer("")).SchemaType(map[string]string{dialect.Postgres: "numeric(19,0)"}).StructTag(`xml:"CHANGEID,attr" json:"change_id"`),
		field.String("number").SchemaType(map[string]string{dialect.Postgres: "text"}).StructTag(`xml:"NUMBER,attr" json:"number"`),
		field.String("opertype_id").SchemaType(map[string]string{dialect.Postgres: "varchar(2)"}).StructTag(`xml:"OPERTYPEID,attr" json:"opertype_id"`),
		field.String("prev_id").GoType(gartype.Integer("")).SchemaType(map[string]string{dialect.Postgres: "numeric(19,0)"}).StructTag(`xml:"PREVID,attr" json:"prev_id,omitempty"`).Optional().Nillable(),
		field.String("next_id").GoType(gartype.Integer("")).SchemaType(map[string]string{dialect.Postgres: "numeric(19,0)"}).StructTag(`xml:"NEXTID,attr" json:"next_id,omitempty"`).Optional().Nillable(),
		field.String("update_date").GoType(gartype.Date("")).SchemaType(map[string]string{dialect.Postgres: "date"}).StructTag(`xml:"UPDATEDATE,attr" json:"update_date"`),
		field.String("start_date").GoType(gartype.Date("")).SchemaType(map[string]string{dialect.Postgres: "date"}).StructTag(`xml:"STARTDATE,attr" json:"start_date"`),
		field.String("end_date").GoType(gartype.Date("")).SchemaType(map[string]string{dialect.Postgres: "date"}).StructTag(`xml:"ENDDATE,attr" json:"end_date"`),
		field.Bool("is_actual").GoType(gartype.Flag(false)).StructTag(`xml:"ISACTUAL,attr" json:"is_actual"`),
		field.Bool("is_active").GoType(gartype.Flag(false)).StructTag(`xml:"ISACTIVE,attr" json:"is_active"`),
	}
}

func (Stead) Annotations() []schema.Annotation {
	return []schema.Annotation{GARFormat{Kind: "AS_STEADS"}, entsql.Annotation{Table: "steads"}}
}

func (Stead) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("object_id").StorageKey("steads_object_id_idx"),
	}
}
