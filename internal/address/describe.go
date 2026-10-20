package address

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"gar-importer/internal/search"
	"gar-importer/internal/storage"
)

// ErrNotFound — объект с таким object_id не найден ни в одной таблице
// адресных объектов ГАР.
var ErrNotFound = errors.New("address: объект не найден")

// Description — описание одного адресного объекта для чтения через API:
// собственные атрибуты записи, полная адресная строка по иерархии и родитель.
type Description struct {
	ObjectID    int64
	ObjectGUID  string
	Kind        string // search.Kind*: addr_obj, house, apartment, room, stead, carplace
	Name        string
	TypeName    string
	Level       int
	Region      string
	FullAddress string
	IsActual    bool
	IsActive    bool
	// ParentObjectID — родительский объект административной иерархии;
	// nil — родителя нет (корень или запись иерархии отсутствует).
	ParentObjectID *int64
}

// Запросы собственной записи объекта по видам (по одной действующей записи
// на object_id, действующая предпочтительнее исторических — как в builder.go).
const (
	describeAddrObjQuery = `SELECT o.object_guid::text, o.name, o.type_name, o.level, o.is_actual, o.is_active
FROM gar.addr_obj o
WHERE o.object_id = $1
ORDER BY o.is_active DESC, o.is_actual DESC, o.end_date DESC, o.id DESC
LIMIT 1`

	describeHousesQuery = `SELECT o.object_guid::text, o.house_num, o.house_type::text,
       o.add_num1, o.add_type1::text, o.add_num2, o.add_type2::text,
       o.is_actual, o.is_active
FROM gar.houses o
WHERE o.object_id = $1
ORDER BY o.is_active DESC, o.is_actual DESC, o.end_date DESC, o.id DESC
LIMIT 1`

	describeApartmentsQuery = `SELECT o.object_guid::text, o.number, o.apart_type, o.is_actual, o.is_active
FROM gar.apartments o
WHERE o.object_id = $1
ORDER BY o.is_active DESC, o.is_actual DESC, o.end_date DESC, o.id DESC
LIMIT 1`

	describeRoomsQuery = `SELECT o.object_guid::text, o.number, o.room_type, o.is_actual, o.is_active
FROM gar.rooms o
WHERE o.object_id = $1
ORDER BY o.is_active DESC, o.is_actual DESC, o.end_date DESC, o.id DESC
LIMIT 1`

	describeSteadsQuery = `SELECT o.object_guid::text, o.number, o.is_actual, o.is_active
FROM gar.steads o
WHERE o.object_id = $1
ORDER BY o.is_active DESC, o.is_actual DESC, o.end_date DESC, o.id DESC
LIMIT 1`

	describeCarplacesQuery = `SELECT o.object_guid::text, o.number, o.is_actual, o.is_active
FROM gar.carplaces o
WHERE o.object_id = $1
ORDER BY o.is_active DESC, o.is_actual DESC, o.end_date DESC, o.id DESC
LIMIT 1`

	// describeHierarchyQuery — административная иерархия или муниципальная при её отсутствии
	// (действующая предпочтительнее, как hierarchySelect в builder.go).
	describeHierarchyQuery = `SELECT h.parent_obj_id,h.path,COALESCE(NULLIF(h.region_code,''),root.region_code)
 FROM (SELECT parent_obj_id,path,region_code FROM (
 SELECT parent_obj_id,path,region_code,is_active,end_date,id,0 AS priority FROM gar.adm_hierarchy WHERE object_id=$1
 UNION ALL
 SELECT parent_obj_id,path,NULL::text,is_active,end_date,id,1 FROM gar.mun_hierarchy WHERE object_id=$1
 ) versions ORDER BY priority,is_active DESC,end_date DESC,id DESC LIMIT 1) h
 ` + rootRegionJoin

	// describeReestrQuery — уровень объекта из реестра.
	describeReestrQuery = `SELECT r.level_id FROM gar.reestr_objects r WHERE r.object_id = $1`

	// childrenQuery — активные дочерние объекты иерархии (по одному
	// идентификатору на объект, исторические дубли схлопнуты).
	childrenQuery = `SELECT DISTINCT h.object_id
FROM gar.adm_hierarchy h
WHERE h.parent_obj_id = $1 AND h.is_active
ORDER BY h.object_id
LIMIT $2`

	// childrenCountQuery — число активных дочерних объектов иерархии.
	childrenCountQuery = `SELECT COUNT(DISTINCT h.object_id)
FROM gar.adm_hierarchy h
WHERE h.parent_obj_id = $1 AND h.is_active`
)

// DescribeObject читает объект objectID из PostgreSQL и собирает его описание:
// вид определяется первой таблицей, содержащей object_id (addr_obj, houses,
// apartments, rooms, steads, carplaces — в этом порядке); полная адресная
// строка строится по пути административной иерархии по тем же правилам,
// что и поисковые документы (builder.go + format.go). Отсутствие объекта —
// ErrNotFound.
func (b *Builder) DescribeObject(ctx context.Context, objectID int64) (*Description, error) {
	self, desc, err := b.loadOwnRecord(ctx, objectID)
	if err != nil {
		return nil, err
	}

	// Иерархия: путь до корня, родитель и регион. Объект без записи
	// иерархии (не должно встречаться в импортированных регионах)
	// описывается собственным компонентом без адресной строки предков.
	var (
		parentID   sql.NullInt64
		path       string
		regionCode sql.NullString
	)
	err = b.store.SQL().QueryRowContext(ctx, describeHierarchyQuery, objectID).
		Scan(&parentID, &path, &regionCode)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return nil, fmt.Errorf("address: иерархия объекта %d: %w", objectID, err)
	default:
		desc.Region = normalizeRegion(regionCode, 0)
		if parentID.Valid {
			id := parentID.Int64
			desc.ParentObjectID = &id
		}
	}

	var reestrLevel sql.NullInt64
	err = b.store.SQL().QueryRowContext(ctx, describeReestrQuery, objectID).Scan(&reestrLevel)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("address: реестр объекта %d: %w", objectID, err)
	}
	desc.Level = resolveLevel(reestrLevel, desc.levelFallback)

	ids := parsePath(path)
	if desc.Kind == search.KindAddrObj {
		ids = append(ids, objectID)
	}
	ancestors, err := b.loadAncestors(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("address: предки объекта %d: %w", objectID, err)
	}
	if desc.Kind == search.KindAddrObj {
		if component, ok := ancestors[objectID]; ok {
			self = component
		}
	}
	raw := rawObject{path: path, self: self}
	raw.doc.ObjectID = strconv.FormatInt(objectID, 10)
	desc.FullAddress = JoinComponents(ancestorChain(raw, ancestors))
	return &desc.Description, nil
}

// ListChildren возвращает идентификаторы активных дочерних объектов
// административной иерархии (parent_obj_id = parentID), не более limit.
func ListChildren(ctx context.Context, store *storage.Store, parentID int64, limit int) ([]int64, error) {
	rows, err := store.SQL().QueryContext(ctx, childrenQuery, parentID, limit)
	if err != nil {
		return nil, fmt.Errorf("address: дети объекта %d: %w", parentID, err)
	}
	ids := []int64{}
	if err := each(rows, func() error {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("address: дети объекта %d: %w", parentID, err)
	}
	return ids, nil
}

// CountChildren возвращает число активных дочерних объектов
// административной иерархии (parent_obj_id = parentID).
func CountChildren(ctx context.Context, store *storage.Store, parentID int64) (int64, error) {
	var count int64
	if err := store.SQL().QueryRowContext(ctx, childrenCountQuery, parentID).Scan(&count); err != nil {
		return 0, fmt.Errorf("address: число детей объекта %d: %w", parentID, err)
	}
	return count, nil
}

// descriptionDraft — Description со служебным полем для передачи
// собственного уровня addr_obj в resolveLevel.
type descriptionDraft struct {
	Description
	levelFallback string
}

// loadOwnRecord ищет собственную запись объекта по таблицам видов в порядке
// addr_obj → houses → apartments → rooms → steads → carplaces и возвращает
// компонент адресной строки и заготовку описания.
func (b *Builder) loadOwnRecord(ctx context.Context, objectID int64) (Component, *descriptionDraft, error) {
	desc := &descriptionDraft{}
	desc.ObjectID = objectID

	// addr_obj: наименование и тип, собственный числовой уровень.
	var ownLevel string
	err := b.store.SQL().QueryRowContext(ctx, describeAddrObjQuery, objectID).
		Scan(&desc.ObjectGUID, &desc.Name, &desc.TypeName, &ownLevel, &desc.IsActual, &desc.IsActive)
	switch {
	case err == nil:
		desc.Kind = search.KindAddrObj
		desc.Name = strings.TrimSpace(desc.Name)
		desc.TypeName = NormalizeShortType(desc.TypeName)
		desc.levelFallback = ownLevel
		return AddrObjComponent(desc.TypeName, desc.Name), desc, nil
	case !errors.Is(err, sql.ErrNoRows):
		return Component{}, nil, fmt.Errorf("address: addr_obj %d: %w", objectID, err)
	}

	// houses: номер с типами (основной и дополнительные).
	var (
		num, houseType, addNum1, addType1 sql.NullString
		addNum2, addType2                 sql.NullString
	)
	err = b.store.SQL().QueryRowContext(ctx, describeHousesQuery, objectID).
		Scan(&desc.ObjectGUID, &num, &houseType, &addNum1, &addType1, &addNum2, &addType2,
			&desc.IsActual, &desc.IsActive)
	switch {
	case err == nil:
		dicts, err := b.dictionaries(ctx)
		if err != nil {
			return Component{}, nil, err
		}
		add1 := NumberedPart{Num: addNum1.String, Type: dicts.addHouseTypes[parseTypeID(addType1)]}
		add2 := NumberedPart{Num: addNum2.String, Type: dicts.addHouseTypes[parseTypeID(addType2)]}
		self := HouseComponent(num.String, dicts.houseTypes[parseTypeID(houseType)], add1, add2)
		desc.Kind = search.KindHouse
		desc.Name = houseName(num.String, addNum1.String, addNum2.String)
		desc.TypeName = self.Type
		return self, desc, nil
	case !errors.Is(err, sql.ErrNoRows):
		return Component{}, nil, fmt.Errorf("address: houses %d: %w", objectID, err)
	}

	// apartments и rooms: номер с типом из справочника.
	if self, found, err := b.loadNumbered(ctx, objectID, describeApartmentsQuery,
		search.KindApartment, defaultApartmentType, true, desc); err != nil || found {
		return self, desc, err
	}
	if self, found, err := b.loadNumbered(ctx, objectID, describeRoomsQuery,
		search.KindRoom, defaultRoomType, true, desc); err != nil || found {
		return self, desc, err
	}

	// steads и carplaces: тип по уровню из ADDR_OBJ_TYPES.
	if self, found, err := b.loadNumbered(ctx, objectID, describeSteadsQuery,
		search.KindStead, "", false, desc); err != nil || found {
		return self, desc, err
	}
	if self, found, err := b.loadNumbered(ctx, objectID, describeCarplacesQuery,
		search.KindCarplace, "", false, desc); err != nil || found {
		return self, desc, err
	}
	return Component{}, nil, fmt.Errorf("%w: object_id %d", ErrNotFound, objectID)
}

// loadNumbered читает запись объекта с простым номером (apartments, rooms,
// steads, carplaces). withType — в запросе есть столбец идентификатора типа.
// found=false означает «записи нет, пробуй следующий вид».
func (b *Builder) loadNumbered(ctx context.Context, objectID int64, query, kind, fallback string,
	withType bool, desc *descriptionDraft) (Component, bool, error) {
	var (
		number string
		typeID sql.NullInt64
	)
	row := b.store.SQL().QueryRowContext(ctx, query, objectID)
	var err error
	if withType {
		err = row.Scan(&desc.ObjectGUID, &number, &typeID, &desc.IsActual, &desc.IsActive)
	} else {
		err = row.Scan(&desc.ObjectGUID, &number, &desc.IsActual, &desc.IsActive)
	}
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Component{}, false, nil
	case err != nil:
		return Component{}, false, fmt.Errorf("address: %s %d: %w", kind, objectID, err)
	}
	typeShort := ""
	if !withType {
		dicts, err := b.dictionaries(ctx)
		if err != nil {
			return Component{}, false, err
		}
		if kind == search.KindStead {
			fallback = dicts.levelTypes[9]
		}
		if kind == search.KindCarplace {
			fallback = dicts.levelTypes[17]
		}
	}
	if withType {
		dicts, err := b.dictionaries(ctx)
		if err != nil {
			return Component{}, false, err
		}
		switch kind {
		case search.KindApartment:
			typeShort = dicts.apartTypes[typeID.Int64]
		case search.KindRoom:
			typeShort = dicts.roomTypes[typeID.Int64]
		}
	}
	self := NumberComponent(typeShort, fallback, number)
	desc.Kind = kind
	desc.Name = strings.TrimSpace(number)
	desc.TypeName = self.Type
	return self, true, nil
}
