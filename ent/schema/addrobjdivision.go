package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// AddrObjDivision описывает запись ГАР; XML и JSON используют генерируемую Ent-модель.
type AddrObjDivision struct{ ent.Schema }

func (AddrObjDivision) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").StructTag(`xml:"ID,attr" json:"id,string"`).Immutable().Annotations(entsql.Annotation{Incremental: new(false)}),
		field.Int64("parent_id").StructTag(`xml:"PARENTID,attr" json:"parent_id,string"`),
		field.Int64("child_id").StructTag(`xml:"CHILDID,attr" json:"child_id,string"`),
		field.Int64("change_id").StructTag(`xml:"CHANGEID,attr" json:"change_id,string"`),
	}
}

func (AddrObjDivision) Annotations() []schema.Annotation {
	return []schema.Annotation{GARFormat{Kind: "AS_ADDR_OBJ_DIVISION"}, entsql.Annotation{Table: "addr_obj_division"}}
}

func (AddrObjDivision) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("parent_id").StorageKey("addr_obj_division_parent_idx"),
		index.Fields("child_id").StorageKey("addr_obj_division_child_idx"),
	}
}
