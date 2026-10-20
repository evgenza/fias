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

// AdmHierarchyItem описывает запись ГАР; XML и JSON используют генерируемую Ent-модель.
type AdmHierarchyItem struct{ ent.Schema }

func (AdmHierarchyItem) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").StructTag(`xml:"ID,attr" json:"id,string"`).Immutable().Annotations(entsql.Annotation{Incremental: new(false)}),
		field.Int64("object_id").StructTag(`xml:"OBJECTID,attr" json:"object_id,string"`),
		field.Int64("parent_obj_id").StructTag(`xml:"PARENTOBJID,attr" json:"parent_obj_id,omitempty,string"`).Optional().Nillable(),
		field.Int64("change_id").StructTag(`xml:"CHANGEID,attr" json:"change_id,string"`),
		field.String("region_code").SchemaType(map[string]string{dialect.Postgres: "varchar(4)"}).StructTag(`xml:"REGIONCODE,attr" json:"region_code,omitempty"`).Optional().Nillable(),
		field.String("area_code").SchemaType(map[string]string{dialect.Postgres: "varchar(4)"}).StructTag(`xml:"AREACODE,attr" json:"area_code,omitempty"`).Optional().Nillable(),
		field.String("city_code").SchemaType(map[string]string{dialect.Postgres: "varchar(4)"}).StructTag(`xml:"CITYCODE,attr" json:"city_code,omitempty"`).Optional().Nillable(),
		field.String("place_code").SchemaType(map[string]string{dialect.Postgres: "varchar(4)"}).StructTag(`xml:"PLACECODE,attr" json:"place_code,omitempty"`).Optional().Nillable(),
		field.String("plan_code").SchemaType(map[string]string{dialect.Postgres: "varchar(4)"}).StructTag(`xml:"PLANCODE,attr" json:"plan_code,omitempty"`).Optional().Nillable(),
		field.String("street_code").SchemaType(map[string]string{dialect.Postgres: "varchar(4)"}).StructTag(`xml:"STREETCODE,attr" json:"street_code,omitempty"`).Optional().Nillable(),
		field.Int64("prev_id").StructTag(`xml:"PREVID,attr" json:"prev_id,omitempty,string"`).Optional().Nillable(),
		field.Int64("next_id").StructTag(`xml:"NEXTID,attr" json:"next_id,omitempty,string"`).Optional().Nillable(),
		field.String("update_date").GoType(gartype.Date("")).SchemaType(map[string]string{dialect.Postgres: "date"}).StructTag(`xml:"UPDATEDATE,attr" json:"update_date"`),
		field.String("start_date").GoType(gartype.Date("")).SchemaType(map[string]string{dialect.Postgres: "date"}).StructTag(`xml:"STARTDATE,attr" json:"start_date"`),
		field.String("end_date").GoType(gartype.Date("")).SchemaType(map[string]string{dialect.Postgres: "date"}).StructTag(`xml:"ENDDATE,attr" json:"end_date"`),
		field.Bool("is_active").GoType(gartype.Flag(false)).StructTag(`xml:"ISACTIVE,attr" json:"is_active"`),
		field.String("path").SchemaType(map[string]string{dialect.Postgres: "text"}).StructTag(`xml:"PATH,attr" json:"path"`),
	}
}

func (AdmHierarchyItem) Annotations() []schema.Annotation {
	return []schema.Annotation{GARFormat{Kind: "AS_ADM_HIERARCHY"}, entsql.Annotation{Table: "adm_hierarchy"}}
}

func (AdmHierarchyItem) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("region_code", "object_id").StorageKey("adm_hierarchy_region_object_idx"),
		index.Fields("object_id", "is_active", "end_date", "id").StorageKey("adm_hierarchy_latest_idx").Annotations(entsql.DescColumns("is_active", "end_date", "id")),
		index.Fields("object_id").StorageKey("adm_hierarchy_object_id_idx"),
		index.Fields("parent_obj_id").StorageKey("adm_hierarchy_parent_idx"),
	}
}
