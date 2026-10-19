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

// House описывает запись ГАР; XML и JSON используют генерируемую Ent-модель.
type House struct{ ent.Schema }

func (House) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").StructTag(`xml:"ID,attr" json:"id,string"`).Immutable().Annotations(entsql.Annotation{Incremental: new(false)}),
		field.Int64("object_id").StructTag(`xml:"OBJECTID,attr" json:"object_id,string"`),
		field.String("object_guid").SchemaType(map[string]string{dialect.Postgres: "uuid"}).StructTag(`xml:"OBJECTGUID,attr" json:"object_guid"`),
		field.Int64("change_id").StructTag(`xml:"CHANGEID,attr" json:"change_id,string"`),
		field.String("house_num").SchemaType(map[string]string{dialect.Postgres: "text"}).StructTag(`xml:"HOUSENUM,attr" json:"house_num,omitempty"`).Optional().Nillable(),
		field.String("add_num1").SchemaType(map[string]string{dialect.Postgres: "text"}).StructTag(`xml:"ADDNUM1,attr" json:"add_num1,omitempty"`).Optional().Nillable(),
		field.String("add_num2").SchemaType(map[string]string{dialect.Postgres: "text"}).StructTag(`xml:"ADDNUM2,attr" json:"add_num2,omitempty"`).Optional().Nillable(),
		field.String("house_type").GoType(gartype.Integer("")).SchemaType(map[string]string{dialect.Postgres: "numeric"}).StructTag(`xml:"HOUSETYPE,attr" json:"house_type,omitempty"`).Optional().Nillable(),
		field.String("add_type1").GoType(gartype.Integer("")).SchemaType(map[string]string{dialect.Postgres: "numeric"}).StructTag(`xml:"ADDTYPE1,attr" json:"add_type1,omitempty"`).Optional().Nillable(),
		field.String("add_type2").GoType(gartype.Integer("")).SchemaType(map[string]string{dialect.Postgres: "numeric"}).StructTag(`xml:"ADDTYPE2,attr" json:"add_type2,omitempty"`).Optional().Nillable(),
		field.String("opertype_id").GoType(gartype.Integer("")).SchemaType(map[string]string{dialect.Postgres: "numeric"}).StructTag(`xml:"OPERTYPEID,attr" json:"opertype_id"`),
		field.Int64("prev_id").StructTag(`xml:"PREVID,attr" json:"prev_id,omitempty,string"`).Optional().Nillable(),
		field.Int64("next_id").StructTag(`xml:"NEXTID,attr" json:"next_id,omitempty,string"`).Optional().Nillable(),
		field.String("update_date").GoType(gartype.Date("")).SchemaType(map[string]string{dialect.Postgres: "date"}).StructTag(`xml:"UPDATEDATE,attr" json:"update_date"`),
		field.String("start_date").GoType(gartype.Date("")).SchemaType(map[string]string{dialect.Postgres: "date"}).StructTag(`xml:"STARTDATE,attr" json:"start_date"`),
		field.String("end_date").GoType(gartype.Date("")).SchemaType(map[string]string{dialect.Postgres: "date"}).StructTag(`xml:"ENDDATE,attr" json:"end_date"`),
		field.Bool("is_actual").GoType(gartype.Flag(false)).StructTag(`xml:"ISACTUAL,attr" json:"is_actual"`),
		field.Bool("is_active").GoType(gartype.Flag(false)).StructTag(`xml:"ISACTIVE,attr" json:"is_active"`),
	}
}

func (House) Annotations() []schema.Annotation {
	return []schema.Annotation{GARFormat{Kind: "AS_HOUSES"}, entsql.Annotation{Table: "houses"}}
}

func (House) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("object_id").StorageKey("houses_object_id_idx"),
	}
}
