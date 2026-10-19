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

// AddrObj описывает запись ГАР; XML и JSON используют генерируемую Ent-модель.
type AddrObj struct{ ent.Schema }

func (AddrObj) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").StructTag(`xml:"ID,attr" json:"id,string"`).Immutable().Annotations(entsql.Annotation{Incremental: new(false)}),
		field.Int64("object_id").StructTag(`xml:"OBJECTID,attr" json:"object_id,string"`),
		field.String("object_guid").SchemaType(map[string]string{dialect.Postgres: "uuid"}).StructTag(`xml:"OBJECTGUID,attr" json:"object_guid"`),
		field.Int64("change_id").StructTag(`xml:"CHANGEID,attr" json:"change_id,string"`),
		field.String("name").SchemaType(map[string]string{dialect.Postgres: "text"}).StructTag(`xml:"NAME,attr" json:"name"`),
		field.String("type_name").SchemaType(map[string]string{dialect.Postgres: "text"}).StructTag(`xml:"TYPENAME,attr" json:"type_name"`),
		field.String("level").SchemaType(map[string]string{dialect.Postgres: "varchar(10)"}).StructTag(`xml:"LEVEL,attr" json:"level"`),
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

func (AddrObj) Annotations() []schema.Annotation {
	return []schema.Annotation{GARFormat{Kind: "AS_ADDR_OBJ"}, entsql.Annotation{Table: "addr_obj"}}
}

func (AddrObj) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("object_id").StorageKey("addr_obj_object_id_idx"),
		index.Fields("level").StorageKey("addr_obj_level_active_idx").Annotations(entsql.IndexWhere("is_actual AND is_active")),
	}
}
