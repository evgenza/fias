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

// ReestrObject описывает запись ГАР; XML и JSON используют генерируемую Ent-модель.
type ReestrObject struct{ ent.Schema }

func (ReestrObject) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").StorageKey("object_id").StructTag(`xml:"OBJECTID,attr" json:"object_id,string"`).Immutable().Annotations(entsql.Annotation{Incremental: new(false)}),
		field.String("create_date").GoType(gartype.Date("")).SchemaType(map[string]string{dialect.Postgres: "date"}).StructTag(`xml:"CREATEDATE,attr" json:"create_date"`),
		field.Int64("change_id").StructTag(`xml:"CHANGEID,attr" json:"change_id,string"`),
		field.Int64("level_id").StructTag(`xml:"LEVELID,attr" json:"level_id,string"`),
		field.String("update_date").GoType(gartype.Date("")).SchemaType(map[string]string{dialect.Postgres: "date"}).StructTag(`xml:"UPDATEDATE,attr" json:"update_date"`),
		field.String("object_guid").SchemaType(map[string]string{dialect.Postgres: "uuid"}).StructTag(`xml:"OBJECTGUID,attr" json:"object_guid"`),
		field.Bool("is_active").GoType(gartype.Flag(false)).StructTag(`xml:"ISACTIVE,attr" json:"is_active"`),
	}
}

func (ReestrObject) Annotations() []schema.Annotation {
	return []schema.Annotation{GARFormat{Kind: "AS_REESTR_OBJECTS"}, entsql.Annotation{Table: "reestr_objects"}}
}

func (ReestrObject) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("object_guid").Unique().StorageKey("reestr_objects_guid_idx"),
	}
}
