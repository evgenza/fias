package address

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"

	"gar-importer/internal/search"
	"gar-importer/internal/storage"
)

// ObjectExists accepts hierarchy-only parents while rejecting unknown IDs.
func ObjectExists(ctx context.Context, store *storage.Store, id int64) (bool, error) {
	var exists bool
	err := store.SQL().QueryRowContext(ctx, `SELECT EXISTS (
 SELECT 1 FROM gar.reestr_objects WHERE object_id=$1
 UNION ALL SELECT 1 FROM gar.adm_hierarchy WHERE object_id=$1
 UNION ALL SELECT 1 FROM gar.mun_hierarchy WHERE object_id=$1
 UNION ALL SELECT 1 FROM gar.addr_obj WHERE object_id=$1
 UNION ALL SELECT 1 FROM gar.houses WHERE object_id=$1
 UNION ALL SELECT 1 FROM gar.apartments WHERE object_id=$1
 UNION ALL SELECT 1 FROM gar.rooms WHERE object_id=$1
 UNION ALL SELECT 1 FROM gar.steads WHERE object_id=$1
 UNION ALL SELECT 1 FROM gar.carplaces WHERE object_id=$1)`, id).Scan(&exists)
	return exists, err
}

type batchOwn struct {
	ObjectGUID string      `json:"object_guid"`
	Name       string      `json:"name"`
	TypeName   string      `json:"type_name"`
	Level      string      `json:"level"`
	Number     string      `json:"number"`
	HouseNum   string      `json:"house_num"`
	AddNum1    string      `json:"add_num1"`
	AddNum2    string      `json:"add_num2"`
	HouseType  json.Number `json:"house_type"`
	AddType1   json.Number `json:"add_type1"`
	AddType2   json.Number `json:"add_type2"`
	ApartType  int64       `json:"apart_type"`
	RoomType   int64       `json:"room_type"`
	IsActual   bool        `json:"is_actual"`
	IsActive   bool        `json:"is_active"`
}

// DescribeObjects loads own records and hierarchy in one query, then shares
// one ancestor load across the whole page. Rows are closed before enrichment.
func (b *Builder) DescribeObjects(ctx context.Context, ids []int64) ([]*Description, error) {
	result := make([]*Description, 0, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	dicts, err := b.dictionaries(ctx)
	if err != nil {
		return nil, err
	}
	query := `SELECT i.id,s.kind,s.own,h.parent_obj_id,h.path,h.region_code,r.level_id
FROM unnest($1::bigint[]) WITH ORDINALITY i(id,ord)
LEFT JOIN gar.reestr_objects r ON r.object_id=i.id
JOIN LATERAL ((SELECT 'addr_obj' AS kind,to_jsonb(o) AS own FROM gar.addr_obj o WHERE o.object_id=i.id AND (r.object_id IS NULL OR r.level_id NOT IN (9,10,11,12,17)) ORDER BY o.is_active DESC,o.is_actual DESC,o.end_date DESC,o.id DESC LIMIT 1)
 UNION ALL
(SELECT 'house' AS kind,to_jsonb(o) AS own FROM gar.houses o WHERE o.object_id=i.id AND (r.object_id IS NULL OR r.level_id=10) ORDER BY o.is_active DESC,o.is_actual DESC,o.end_date DESC,o.id DESC LIMIT 1)
 UNION ALL
(SELECT 'apartment' AS kind,to_jsonb(o) AS own FROM gar.apartments o WHERE o.object_id=i.id AND (r.object_id IS NULL OR r.level_id=11) ORDER BY o.is_active DESC,o.is_actual DESC,o.end_date DESC,o.id DESC LIMIT 1)
 UNION ALL
(SELECT 'room' AS kind,to_jsonb(o) AS own FROM gar.rooms o WHERE o.object_id=i.id AND (r.object_id IS NULL OR r.level_id=12) ORDER BY o.is_active DESC,o.is_actual DESC,o.end_date DESC,o.id DESC LIMIT 1)
 UNION ALL
(SELECT 'stead' AS kind,to_jsonb(o) AS own FROM gar.steads o WHERE o.object_id=i.id AND (r.object_id IS NULL OR r.level_id=9) ORDER BY o.is_active DESC,o.is_actual DESC,o.end_date DESC,o.id DESC LIMIT 1)
 UNION ALL
(SELECT 'carplace' AS kind,to_jsonb(o) AS own FROM gar.carplaces o WHERE o.object_id=i.id AND (r.object_id IS NULL OR r.level_id=17) ORDER BY o.is_active DESC,o.is_actual DESC,o.end_date DESC,o.id DESC LIMIT 1)) s ON true
LEFT JOIN LATERAL (` + strings.ReplaceAll(strings.ReplaceAll(describeHierarchyQuery, "COALESCE(NULLIF(h.region_code,''),root.region_code)", "COALESCE(NULLIF(h.region_code,''),root.region_code) AS region_code"), "$1", "i.id") + `) h ON true
ORDER BY i.ord`
	rows, err := b.store.SQL().QueryContext(ctx, query, ids)
	if err != nil {
		return nil, err
	}
	var raws []rawObject
	var ancestorIDs []int64
	err = each(rows, func() error {
		var id int64
		var kind string
		var data []byte
		var parent, level sql.NullInt64
		var path, region sql.NullString
		if err := rows.Scan(&id, &kind, &data, &parent, &path, &region, &level); err != nil {
			return err
		}
		var own batchOwn
		if err := json.Unmarshal(data, &own); err != nil {
			return err
		}
		d := &Description{ObjectID: id, ObjectGUID: own.ObjectGUID, Kind: kind, Name: strings.TrimSpace(own.Name), TypeName: NormalizeShortType(own.TypeName), Level: resolveLevel(level, own.Level), Region: normalizeRegion(region, 0), IsActual: own.IsActual, IsActive: own.IsActive}
		if !path.Valid {
			d.Region = ""
		}
		if parent.Valid {
			v := parent.Int64
			d.ParentObjectID = &v
		}
		var self Component
		switch kind {
		case search.KindAddrObj:
			self = AddrObjComponent(d.TypeName, d.Name)
		case search.KindHouse:
			ht, _ := own.HouseType.Int64()
			a1, _ := own.AddType1.Int64()
			a2, _ := own.AddType2.Int64()
			self = HouseComponent(own.HouseNum, dicts.houseTypes[ht], NumberedPart{Num: own.AddNum1, Type: dicts.addHouseTypes[a1]}, NumberedPart{Num: own.AddNum2, Type: dicts.addHouseTypes[a2]})
			d.Name = houseName(own.HouseNum, own.AddNum1, own.AddNum2)
			d.TypeName = self.Type
		default:
			typ, fallback := "", ""
			switch kind {
			case search.KindApartment:
				typ, fallback = dicts.apartTypes[own.ApartType], defaultApartmentType
			case search.KindRoom:
				typ, fallback = dicts.roomTypes[own.RoomType], defaultRoomType
			case search.KindStead:
				fallback = dicts.levelTypes[9]
			case search.KindCarplace:
				fallback = dicts.levelTypes[17]
			}
			self = NumberComponent(typ, fallback, own.Number)
			d.Name = strings.TrimSpace(own.Number)
			d.TypeName = self.Type
		}
		raw := rawObject{path: path.String, self: self}
		raw.doc.ObjectID = strconv.FormatInt(id, 10)
		raws = append(raws, raw)
		result = append(result, d)
		ancestorIDs = append(ancestorIDs, parsePath(path.String)...)
		if kind == search.KindAddrObj {
			ancestorIDs = append(ancestorIDs, id)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	ancestors, err := b.loadAncestors(ctx, ancestorIDs)
	if err != nil {
		return nil, err
	}
	for i, d := range result {
		if d.Kind == search.KindAddrObj {
			if self, ok := ancestors[d.ObjectID]; ok {
				raws[i].self = self
			}
		}
		d.FullAddress = JoinComponents(ancestorChain(raws[i], ancestors))
	}
	return result, nil
}
