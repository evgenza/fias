package address

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"gar-importer/internal/search"
	"gar-importer/internal/storage"
)

// BatchSize — число объектов в одной пачке документов, передаваемой в sink.
const BatchSize = 2000

// Иерархия для построения адресных строк: административная (adm_hierarchy).
// По руководству ГАР PATH есть в обеих иерархиях и содержит полную цепочку
// OBJECTID предков включая сам объект; adm_hierarchy даёт адрес в
// административно-территориальном делении (как в примерах контракта).
const hierarchyTable = "gar.adm_hierarchy"

// hierarchySelect — снимок иерархии региона: по одной записи на объект
// (действующая предпочтительнее исторических).
const hierarchySelect = `SELECT DISTINCT ON (h.object_id) h.object_id, h.path, h.region_code
FROM ` + hierarchyTable + ` h
WHERE h.region_code ~ '^[0-9]+$' AND h.region_code::int = $1
ORDER BY h.object_id, h.is_active DESC, h.end_date DESC, h.id DESC`

// Запросы объектов по видам: собственная запись (одна на object_id,
// действующая предпочтительнее), иерархия региона и уровень из реестра.
const (
	addrObjQuery = `SELECT o.object_id, o.object_guid::text, o.name, o.type_name, o.level,
       o.is_actual, o.is_active, h.path, h.region_code, r.level_id
FROM (SELECT DISTINCT ON (object_id) object_id, object_guid, name, type_name, level, is_actual, is_active
      FROM gar.addr_obj
      ORDER BY object_id, is_active DESC, is_actual DESC, end_date DESC, id DESC) o
JOIN (` + hierarchySelect + `) h ON h.object_id = o.object_id
LEFT JOIN gar.reestr_objects r ON r.object_id = o.object_id
ORDER BY o.object_id`

	housesQuery = `SELECT o.object_id, o.object_guid::text, o.house_num, o.house_type,
       o.add_num1, o.add_type1, o.add_num2, o.add_type2,
       o.is_actual, o.is_active, h.path, h.region_code, r.level_id
FROM (SELECT DISTINCT ON (object_id) object_id, object_guid, house_num,
             house_type::text AS house_type,
             add_num1, add_type1::text AS add_type1,
             add_num2, add_type2::text AS add_type2,
             is_actual, is_active
      FROM gar.houses
      ORDER BY object_id, is_active DESC, is_actual DESC, end_date DESC, id DESC) o
JOIN (` + hierarchySelect + `) h ON h.object_id = o.object_id
LEFT JOIN gar.reestr_objects r ON r.object_id = o.object_id
ORDER BY o.object_id`

	apartmentsQuery = `SELECT o.object_id, o.object_guid::text, o.number, o.apart_type,
       o.is_actual, o.is_active, h.path, h.region_code, r.level_id
FROM (SELECT DISTINCT ON (object_id) object_id, object_guid, number, apart_type, is_actual, is_active
      FROM gar.apartments
      ORDER BY object_id, is_active DESC, is_actual DESC, end_date DESC, id DESC) o
JOIN (` + hierarchySelect + `) h ON h.object_id = o.object_id
LEFT JOIN gar.reestr_objects r ON r.object_id = o.object_id
ORDER BY o.object_id`

	roomsQuery = `SELECT o.object_id, o.object_guid::text, o.number, o.room_type,
       o.is_actual, o.is_active, h.path, h.region_code, r.level_id
FROM (SELECT DISTINCT ON (object_id) object_id, object_guid, number, room_type, is_actual, is_active
      FROM gar.rooms
      ORDER BY object_id, is_active DESC, is_actual DESC, end_date DESC, id DESC) o
JOIN (` + hierarchySelect + `) h ON h.object_id = o.object_id
LEFT JOIN gar.reestr_objects r ON r.object_id = o.object_id
ORDER BY o.object_id`

	steadsQuery = `SELECT o.object_id, o.object_guid::text, o.number,
       o.is_actual, o.is_active, h.path, h.region_code, r.level_id
FROM (SELECT DISTINCT ON (s.object_id) s.object_id::bigint AS object_id, s.object_guid, s.number, s.is_actual, s.is_active
      FROM gar.steads s
      ORDER BY s.object_id, s.is_active DESC, s.is_actual DESC, s.end_date DESC, s.id DESC) o
JOIN (` + hierarchySelect + `) h ON h.object_id = o.object_id
LEFT JOIN gar.reestr_objects r ON r.object_id = o.object_id
ORDER BY o.object_id`

	carplacesQuery = `SELECT o.object_id, o.object_guid::text, o.number,
       o.is_actual, o.is_active, h.path, h.region_code, r.level_id
FROM (SELECT DISTINCT ON (object_id) object_id, object_guid, number, is_actual, is_active
      FROM gar.carplaces
      ORDER BY object_id, is_active DESC, is_actual DESC, end_date DESC, id DESC) o
JOIN (` + hierarchySelect + `) h ON h.object_id = o.object_id
LEFT JOIN gar.reestr_objects r ON r.object_id = o.object_id
ORDER BY o.object_id`
)

// Запросы предков для обогащения пачки: по одной записи на object_id.
const (
	ancestorAddrObjQuery = `SELECT DISTINCT ON (object_id) object_id, name, type_name
FROM gar.addr_obj
WHERE object_id = ANY($1)
ORDER BY object_id, is_active DESC, is_actual DESC, end_date DESC, id DESC`

	ancestorHousesQuery = `SELECT DISTINCT ON (object_id) object_id, house_num,
       house_type::text AS house_type, add_num1, add_type1::text AS add_type1,
       add_num2, add_type2::text AS add_type2
FROM gar.houses
WHERE object_id = ANY($1)
ORDER BY object_id, is_active DESC, is_actual DESC, end_date DESC, id DESC`

	ancestorApartmentsQuery = `SELECT DISTINCT ON (object_id) object_id, number, apart_type
FROM gar.apartments
WHERE object_id = ANY($1)
ORDER BY object_id, is_active DESC, is_actual DESC, end_date DESC, id DESC`
)

// dictionaries — короткие типы из справочников ГАР (id → сокращение).
type dictionaries struct {
	houseTypes    map[int64]string
	addHouseTypes map[int64]string
	apartTypes    map[int64]string
	roomTypes     map[int64]string
}

// Builder строит поисковые документы адресов по данным PostgreSQL.
type Builder struct {
	store         *storage.Store
	dicts         *dictionaries
	dictsMu       sync.Mutex
	dictsLoadedAt time.Time
}

// NewBuilder создаёт сборщика поверх хранилища.
func NewBuilder(store *storage.Store) *Builder {
	return &Builder{store: store}
}

// rawObject — запись объекта, прочитанная из БД, до обогащения предками.
type rawObject struct {
	doc  search.Document // заполнены все поля, кроме FullAddress и IndexedAt
	path string          // материализованный путь («A.B.H»)
	self Component       // собственный компонент адресной строки
}

// BuildAddrObjects строит документы адресообразующих объектов региона;
// пачки по BatchSize документов передаются в sink.
func (b *Builder) BuildAddrObjects(ctx context.Context, region int, sink func([]search.Document) error) error {
	return b.stream(ctx, region, search.KindAddrObj, addrObjQuery,
		func(rows *sql.Rows) (rawObject, error) {
			var (
				obj             rawObject
				objectID        int64
				guid, name, typ string
				ownLevel        string
				isActual        bool
				isActive        bool
				regionCode      sql.NullString
				reestrLevel     sql.NullInt64
			)
			if err := rows.Scan(&objectID, &guid, &name, &typ, &ownLevel,
				&isActual, &isActive, &obj.path, &regionCode, &reestrLevel); err != nil {
				return rawObject{}, err
			}
			obj.self = AddrObjComponent(typ, name)
			obj.doc = search.Document{
				ObjectID:   strconv.FormatInt(objectID, 10),
				ObjectGUID: guid,
				Kind:       search.KindAddrObj,
				Name:       strings.TrimSpace(name),
				TypeName:   NormalizeShortType(typ),
				Region:     normalizeRegion(regionCode, region),
				Level:      resolveLevel(reestrLevel, ownLevel),
				IsActual:   isActual,
				IsActive:   isActive,
			}
			return obj, nil
		}, sink)
}

// BuildHouses строит документы домов региона.
func (b *Builder) BuildHouses(ctx context.Context, region int, sink func([]search.Document) error) error {
	dicts, err := b.dictionaries(ctx)
	if err != nil {
		return err
	}
	return b.stream(ctx, region, search.KindHouse, housesQuery,
		func(rows *sql.Rows) (rawObject, error) {
			var (
				obj                               rawObject
				objectID                          int64
				guid                              string
				num, houseType, addNum1, addType1 sql.NullString
				addNum2, addType2                 sql.NullString
				isActual, isActive                bool
				regionCode                        sql.NullString
				reestrLevel                       sql.NullInt64
			)
			if err := rows.Scan(&objectID, &guid, &num, &houseType,
				&addNum1, &addType1, &addNum2, &addType2,
				&isActual, &isActive, &obj.path, &regionCode, &reestrLevel); err != nil {
				return rawObject{}, err
			}
			add1 := NumberedPart{Num: addNum1.String, Type: dicts.addHouseTypes[parseTypeID(addType1)]}
			add2 := NumberedPart{Num: addNum2.String, Type: dicts.addHouseTypes[parseTypeID(addType2)]}
			obj.self = HouseComponent(num.String, dicts.houseTypes[parseTypeID(houseType)], add1, add2)
			obj.doc = search.Document{
				ObjectID:   strconv.FormatInt(objectID, 10),
				ObjectGUID: guid,
				Kind:       search.KindHouse,
				Name:       strings.TrimSpace(num.String),
				TypeName:   obj.self.Type,
				Region:     normalizeRegion(regionCode, region),
				Level:      resolveLevel(reestrLevel, ""),
				IsActual:   isActual,
				IsActive:   isActive,
			}
			return obj, nil
		}, sink)
}

// BuildApartments строит документы помещений региона.
func (b *Builder) BuildApartments(ctx context.Context, region int, sink func([]search.Document) error) error {
	dicts, err := b.dictionaries(ctx)
	if err != nil {
		return err
	}
	return b.stream(ctx, region, search.KindApartment, apartmentsQuery,
		numberScanner(dicts.apartTypes, defaultApartmentType, search.KindApartment, region), sink)
}

// BuildRooms строит документы комнат региона.
func (b *Builder) BuildRooms(ctx context.Context, region int, sink func([]search.Document) error) error {
	dicts, err := b.dictionaries(ctx)
	if err != nil {
		return err
	}
	return b.stream(ctx, region, search.KindRoom, roomsQuery,
		numberScanner(dicts.roomTypes, defaultRoomType, search.KindRoom, region), sink)
}

// BuildSteads строит документы земельных участков региона.
func (b *Builder) BuildSteads(ctx context.Context, region int, sink func([]search.Document) error) error {
	return b.stream(ctx, region, search.KindStead, steadsQuery,
		numberScanner(nil, defaultSteadType, search.KindStead, region), sink)
}

// BuildCarplaces строит документы машино-мест региона.
func (b *Builder) BuildCarplaces(ctx context.Context, region int, sink func([]search.Document) error) error {
	return b.stream(ctx, region, search.KindCarplace, carplacesQuery,
		numberScanner(nil, defaultCarplaceType, search.KindCarplace, region), sink)
}

// numberScanner — сканер для видов с простым номером (apartments, rooms,
// steads, carplaces): object_id, guid, number, type id (только apartments
// и rooms; для остальных в запросе его нет — см. запросы).
func numberScanner(types map[int64]string, fallback, kind string, region int) func(*sql.Rows) (rawObject, error) {
	withType := types != nil
	return func(rows *sql.Rows) (rawObject, error) {
		var (
			obj                rawObject
			objectID           int64
			guid, number       string
			typeID             sql.NullInt64
			isActual, isActive bool
			regionCode         sql.NullString
			reestrLevel        sql.NullInt64
		)
		var err error
		if withType {
			err = rows.Scan(&objectID, &guid, &number, &typeID,
				&isActual, &isActive, &obj.path, &regionCode, &reestrLevel)
		} else {
			err = rows.Scan(&objectID, &guid, &number,
				&isActual, &isActive, &obj.path, &regionCode, &reestrLevel)
		}
		if err != nil {
			return rawObject{}, err
		}
		obj.self = NumberComponent(types[typeID.Int64], fallback, number)
		obj.doc = search.Document{
			ObjectID:   strconv.FormatInt(objectID, 10),
			ObjectGUID: guid,
			Kind:       kind,
			Name:       strings.TrimSpace(number),
			TypeName:   obj.self.Type,
			Region:     normalizeRegion(regionCode, region),
			Level:      resolveLevel(reestrLevel, ""),
			IsActual:   isActual,
			IsActive:   isActive,
		}
		return obj, nil
	}
}

// stream читает объекты региона потоково и сбрасывает пачки документов
// в sink после обогащения предками.
func (b *Builder) stream(ctx context.Context, region int, kind, query string,
	scan func(*sql.Rows) (rawObject, error), sink func([]search.Document) error) error {
	rows, err := b.store.SQL().QueryContext(ctx, query, region)
	if err != nil {
		return fmt.Errorf("address: выборка %s региона %d: %w", kind, region, err)
	}
	defer rows.Close()
	batch := make([]rawObject, 0, BatchSize)
	for rows.Next() {
		obj, err := scan(rows)
		if err != nil {
			return fmt.Errorf("address: чтение %s региона %d: %w", kind, region, err)
		}
		batch = append(batch, obj)
		if len(batch) == BatchSize {
			if err := b.flush(ctx, region, kind, batch, sink); err != nil {
				return err
			}
			batch = batch[:0]
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("address: выборка %s региона %d: %w", kind, region, err)
	}
	if len(batch) > 0 {
		return b.flush(ctx, region, kind, batch, sink)
	}
	return nil
}

// flush обогащает пачку предками, собирает адресные строки и отдаёт документы.
func (b *Builder) flush(ctx context.Context, region int, kind string, batch []rawObject, sink func([]search.Document) error) error {
	ancestors, err := b.loadAncestors(ctx, collectPathIDs(batch))
	if err != nil {
		return fmt.Errorf("address: предки %s региона %d: %w", kind, region, err)
	}
	indexedAt := time.Now().UTC()
	docs := make([]search.Document, 0, len(batch))
	for _, obj := range batch {
		obj.doc.FullAddress = JoinComponents(ancestorChain(obj, ancestors))
		obj.doc.IndexedAt = indexedAt
		docs = append(docs, obj.doc)
	}
	if err := sink(docs); err != nil {
		return fmt.Errorf("address: запись пачки %s региона %d: %w", kind, region, err)
	}
	return nil
}

// collectPathIDs собирает все уникальные OBJECTID из путей пачки.
func collectPathIDs(batch []rawObject) []int64 {
	seen := make(map[int64]struct{})
	var ids []int64
	for _, obj := range batch {
		for _, id := range parsePath(obj.path) {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	return ids
}

// ancestorChain возвращает компоненты пути объекта: предки из справочных
// карт, собственный компонент — в конце.
func ancestorChain(obj rawObject, ancestors map[int64]Component) []Component {
	ownID, _ := strconv.ParseInt(obj.doc.ObjectID, 10, 64)
	var chain []Component
	for _, id := range parsePath(obj.path) {
		if id == ownID {
			continue
		}
		if component, ok := ancestors[id]; ok {
			chain = append(chain, component)
		}
	}
	return append(chain, obj.self)
}

// parsePath разбирает материализованный путь «A.B.C» в идентификаторы.
func parsePath(path string) []int64 {
	var ids []int64
	for _, token := range strings.Split(path, ".") {
		if id, err := strconv.ParseInt(token, 10, 64); err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

// loadAncestors загружает компоненты предков пачки из addr_obj, houses
// и apartments (комнаты наследуют помещения, помещения — дома).
func (b *Builder) loadAncestors(ctx context.Context, ids []int64) (map[int64]Component, error) {
	components := make(map[int64]Component, len(ids))
	if len(ids) == 0 {
		return components, nil
	}
	dicts, err := b.dictionaries(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := b.store.SQL().QueryContext(ctx, ancestorAddrObjQuery, ids)
	if err != nil {
		return nil, fmt.Errorf("address: предки addr_obj: %w", err)
	}
	if err := each(rows, func() error {
		var id int64
		var name, typ string
		if err := rows.Scan(&id, &name, &typ); err != nil {
			return err
		}
		components[id] = AddrObjComponent(typ, name)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("address: предки addr_obj: %w", err)
	}

	rows, err = b.store.SQL().QueryContext(ctx, ancestorHousesQuery, ids)
	if err != nil {
		return nil, fmt.Errorf("address: предки houses: %w", err)
	}
	if err := each(rows, func() error {
		var (
			id                                int64
			num, houseType, addNum1, addType1 sql.NullString
			addNum2, addType2                 sql.NullString
		)
		if err := rows.Scan(&id, &num, &houseType, &addNum1, &addType1, &addNum2, &addType2); err != nil {
			return err
		}
		add1 := NumberedPart{Num: addNum1.String, Type: dicts.addHouseTypes[parseTypeID(addType1)]}
		add2 := NumberedPart{Num: addNum2.String, Type: dicts.addHouseTypes[parseTypeID(addType2)]}
		components[id] = HouseComponent(num.String, dicts.houseTypes[parseTypeID(houseType)], add1, add2)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("address: предки houses: %w", err)
	}

	rows, err = b.store.SQL().QueryContext(ctx, ancestorApartmentsQuery, ids)
	if err != nil {
		return nil, fmt.Errorf("address: предки apartments: %w", err)
	}
	if err := each(rows, func() error {
		var (
			id     int64
			number string
			typeID int64
		)
		if err := rows.Scan(&id, &number, &typeID); err != nil {
			return err
		}
		components[id] = NumberComponent(dicts.apartTypes[typeID], defaultApartmentType, number)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("address: предки apartments: %w", err)
	}
	return components, nil
}

// each проходит по строкам и закрывает курсор; fn читает текущую строку.
func each(rows *sql.Rows, fn func() error) error {
	defer rows.Close()
	for rows.Next() {
		if err := fn(); err != nil {
			return err
		}
	}
	return rows.Err()
}

// DictionaryTTL bounds staleness for API processes without an event subscription.
const DictionaryTTL = time.Minute

// InvalidateDictionaries invalidates the immutable dictionary snapshot.
func (b *Builder) InvalidateDictionaries() {
	b.dictsMu.Lock()
	defer b.dictsMu.Unlock()
	b.dicts = nil
}

// dictionaries serializes loading and publishes immutable snapshots.
func (b *Builder) dictionaries(ctx context.Context) (*dictionaries, error) {
	b.dictsMu.Lock()
	defer b.dictsMu.Unlock()
	if b.dicts != nil && time.Since(b.dictsLoadedAt) < DictionaryTTL {
		return b.dicts, nil
	}
	dicts := &dictionaries{}
	var err error
	if dicts.houseTypes, err = b.loadTypeDict(ctx, "gar.house_types"); err != nil {
		return nil, err
	}
	if dicts.addHouseTypes, err = b.loadTypeDict(ctx, "gar.add_house_types"); err != nil {
		return nil, err
	}
	if dicts.apartTypes, err = b.loadTypeDict(ctx, "gar.apartment_types"); err != nil {
		return nil, err
	}
	if dicts.roomTypes, err = b.loadTypeDict(ctx, "gar.room_types"); err != nil {
		return nil, err
	}
	// Empty startup results must be retried on the next request.
	if len(dicts.houseTypes)+len(dicts.addHouseTypes)+len(dicts.apartTypes)+len(dicts.roomTypes) > 0 {
		b.dicts = dicts
		b.dictsLoadedAt = time.Now()
	} else {
		b.dicts = nil
	}
	return dicts, nil
}

// loadTypeDict читает справочник типов: id → нормализованное сокращение.
func (b *Builder) loadTypeDict(ctx context.Context, table string) (map[int64]string, error) {
	rows, err := b.store.SQL().QueryContext(ctx,
		`SELECT id, shortname FROM `+table) // таблицы — константы кода, не ввод пользователя
	if err != nil {
		return nil, fmt.Errorf("address: справочник %s: %w", table, err)
	}
	dict := map[int64]string{}
	if err := each(rows, func() error {
		var (
			id    int64
			short sql.NullString
		)
		if err := rows.Scan(&id, &short); err != nil {
			return err
		}
		if normalized := NormalizeShortType(short.String); normalized != "" {
			dict[id] = normalized
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("address: справочник %s: %w", table, err)
	}
	return dict, nil
}

// parseTypeID разбирает идентификатор типа (numeric как текст); мусор → 0.
func parseTypeID(value sql.NullString) int64 {
	if !value.Valid {
		return 0
	}
	id, _ := strconv.ParseInt(strings.TrimSpace(value.String), 10, 64)
	return id
}

// normalizeRegion приводит код региона из иерархии к виду без ведущих
// нулей («077» → «77»); пустой/неверный код заменяется кодом запроса.
func normalizeRegion(code sql.NullString, fallback int) string {
	if code.Valid {
		if parsed, err := strconv.Atoi(strings.TrimSpace(code.String)); err == nil {
			return strconv.Itoa(parsed)
		}
	}
	return strconv.Itoa(fallback)
}

// resolveLevel берёт уровень из реестра объектов; при его отсутствии —
// числовой level собственной записи addr_obj, иначе 0.
func resolveLevel(reestrLevel sql.NullInt64, ownLevel string) int {
	if reestrLevel.Valid {
		return int(reestrLevel.Int64)
	}
	level, _ := strconv.Atoi(strings.TrimSpace(ownLevel))
	return level
}
