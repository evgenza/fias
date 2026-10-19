package schema

import (
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
)

// AddHouseType — справочник AS_ADDHOUSE_TYPES из реальной выгрузки.
// Он использует структуру HOUSETYPES / HOUSETYPE, как AS_HOUSE_TYPES.
type AddHouseType struct{ HouseType }

func (AddHouseType) Annotations() []schema.Annotation {
	return []schema.Annotation{GARFormat{Kind: "AS_ADDHOUSE_TYPES"}, entsql.Annotation{Table: "add_house_types"}}
}
