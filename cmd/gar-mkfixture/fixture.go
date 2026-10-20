package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"gar-importer/internal/gar"
)

// Даты фикстуры фиксированы: повторная генерация обязана давать
// побайтно тот же архив. endDate — принятый в ГАР «бесконечный» срок действия.
const (
	fileDate = "20251019"   // дата выгрузки в именах файлов (ГГГГММДД)
	fileSeq  = "1"          // идентификационный номер файла в выпуске
	dataDate = "2025-10-19" // UPDATEDATE/STARTDATE записей (xs:date)
	endDate  = "2079-06-06" // ENDDATE действующих записей
	changeID = 1
)

// fixtureFile — один XML-файл архива: путь, содержимое и число записей.
type fixtureFile struct {
	name    string
	content string
	records int
}

// Адресные объекты региона: регион (уровень 1) → город (уровень 4) → улицы
// (уровень 5). OBJECTID стабильны и едины для всей фикстуры: от них строятся
// PATH иерархии и записи реестра.
var addrObjects = []struct {
	id     int64
	name   string
	typ    string
	level  string
	parent int64
	path   string
}{
	{101, "Фиксионская", "Респ", "1", 0, "101"},
	{102, "Фиксион", "г", "4", 101, "101.102"},
	{103, "Первая", "ул", "5", 102, "101.102.103"},
	{104, "Вторая", "ул", "5", 102, "101.102.104"},
	{105, "Тестовый", "пр-кт", "5", 102, "101.102.105"},
	{106, "Тихий", "пер", "5", 102, "101.102.106"},
}

var houses = []struct {
	id       int64
	street   int64
	path     string
	num      string
	addNum1  string
	addType1 int
	houseTyp int
}{
	{201, 103, "101.102.103.201", "7", "", 0, 1},
	{202, 103, "101.102.103.202", "9", "1", 1, 1},
	{203, 104, "101.102.104.203", "15", "", 0, 5},
	{204, 105, "101.102.105.204", "1", "А", 3, 1},
	{205, 105, "101.102.105.205", "3", "", 0, 7},
}

var apartments = []struct {
	id    int64
	house int64
	path  string
	num   string
	typ   int
}{
	{301, 201, "101.102.103.201.301", "1", 2},
	{302, 201, "101.102.103.201.302", "2", 2},
	{303, 202, "101.102.103.202.303", "10", 2},
	{304, 203, "101.102.104.203.304", "101", 4},
}

var rooms = []struct {
	id   int64
	apt  int64
	path string
	num  string
	typ  int
}{
	{306, 301, "101.102.103.201.301.306", "1", 1},
	{307, 301, "101.102.103.201.301.307", "2", 1},
}

const (
	steadID      = 401 // участок на городе
	steadPath    = "101.102.401"
	carplaceID   = 501 // машино-место в доме 203
	carplacePath = "101.102.104.203.501"
)

// Уровни объектов (AS_OBJECT_LEVELS) для LEVELID записей реестра.
const (
	levelRegion    = 1
	levelCity      = 4
	levelStreet    = 5
	levelStead     = 9
	levelHouse     = 10
	levelApartment = 11
	levelRoom      = 12
	levelCarplace  = 17
)

// buildFixture собирает все файлы фикстуры: корневые справочники и данные
// одного региона (подкаталог NN/). Порядок файлов и записей фиксирован.
func buildFixture(region int) ([]fixtureFile, error) {
	if region < 1 || region > 99 {
		return nil, fmt.Errorf("код региона должен быть от 1 до 99, получено %d", region)
	}
	regionDir := fmt.Sprintf("%02d/", region)
	regionCode := fmt.Sprintf("%02d", region)

	var files []fixtureFile
	add := func(dir string, kind gar.Kind, records []string) {
		name := fmt.Sprintf("%s%s_%s_%s.XML", dir, kind, fileDate, fileSeq)
		files = append(files, fixtureFile{name: name, content: document(kind, records), records: len(records)})
	}

	// Корневые (общероссийские) справочники.
	add("", gar.KindHouseTypes, houseTypes())
	add("", gar.KindAddHouseTypes, addHouseTypes())
	add("", gar.KindApartmentTypes, apartmentTypes())
	add("", gar.KindRoomTypes, roomTypes())
	add("", gar.KindAddrObjTypes, addrObjTypes())
	add("", gar.KindObjectLevels, objectLevels())
	add("", gar.KindOperationTypes, operationTypes())
	add("", gar.KindParamTypes, paramTypes())
	add("", gar.KindNormativeDocKinds, normativeDocKinds())
	add("", gar.KindNormativeDocTypes, normativeDocTypes())

	// Данные региона.
	add(regionDir, gar.KindAddrObj, addrObjRecords())
	add(regionDir, gar.KindAdmHierarchy, admHierarchy(regionCode))
	add(regionDir, gar.KindHouses, houseRecords())
	add(regionDir, gar.KindApartments, apartmentRecords())
	add(regionDir, gar.KindRooms, roomRecords())
	add(regionDir, gar.KindSteads, steadRecords())
	add(regionDir, gar.KindCarplaces, carplaceRecords())
	add(regionDir, gar.KindReestrObjects, reestrObjects())
	addParams := addrObjParams()
	name := fmt.Sprintf("%sAS_ADDR_OBJ_PARAMS_%s_%s.XML", regionDir, fileDate, fileSeq)
	files = append(files, fixtureFile{name: name, content: document(gar.KindParam, addParams), records: len(addParams)})

	return files, nil
}

// document собирает XML-документ формата: корневой элемент и записи.
func document(kind gar.Kind, records []string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	b.WriteByte('<')
	b.WriteString(kind.RootElement())
	b.WriteByte('>')
	for _, record := range records {
		b.WriteString(record)
	}
	b.WriteString("</")
	b.WriteString(kind.RootElement())
	b.WriteByte('>')
	return b.String()
}

// attrEscaper экранирует специальные символы XML в значениях атрибутов.
var attrEscaper = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	`"`, "&quot;",
)

func esc(value string) string { return attrEscaper.Replace(value) }

// objectGUID строит детерминированный UUID v4 из OBJECTID (sha256 с битами
// версии и варианта RFC 4122): повторная генерация даёт те же идентификаторы.
func objectGUID(id int64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("gar-mkfixture:%d", id)))
	sum[6] = sum[6]&0x0f | 0x40
	sum[8] = sum[8]&0x3f | 0x80
	s := hex.EncodeToString(sum[:16])
	return s[0:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:32]
}

// Справочник домов: типы из реальной выгрузки ГАР.
func houseTypes() []string {
	types := []struct {
		id    int
		name  string
		short string
	}{
		{1, "Дом", "д"},
		{2, "Владение", "влд"},
		{5, "Здание", "зд."},
		{7, "Строение", "стр."},
		{8, "Сооружение", "соор."},
	}
	records := make([]string, 0, len(types))
	for _, t := range types {
		records = append(records, fmt.Sprintf(
			`<HOUSETYPE ID="%d" NAME="%s" SHORTNAME="%s" UPDATEDATE="%s" STARTDATE="%s" ENDDATE="%s" ISACTIVE="true"/>`,
			t.id, esc(t.name), esc(t.short), dataDate, dataDate, endDate))
	}
	return records
}

// Дополнительные типы домов (корпуса, строения, литеры).
func addHouseTypes() []string {
	types := []struct {
		id    int
		name  string
		short string
	}{
		{1, "Корпус", "к"},
		{2, "Строение", "стр."},
		{3, "Литера", "литера"},
		{4, "Сооружение", "соор."},
	}
	records := make([]string, 0, len(types))
	for _, t := range types {
		records = append(records, fmt.Sprintf(
			`<HOUSETYPE ID="%d" NAME="%s" SHORTNAME="%s" UPDATEDATE="%s" STARTDATE="%s" ENDDATE="%s" ISACTIVE="true"/>`,
			t.id, esc(t.name), esc(t.short), dataDate, dataDate, endDate))
	}
	return records
}

func apartmentTypes() []string {
	types := []struct {
		id    int
		name  string
		short string
	}{
		{1, "Помещение", "помещ"},
		{2, "Квартира", "кв"},
		{3, "Комната", "ком"},
		{4, "Офис", "оф"},
	}
	records := make([]string, 0, len(types))
	for _, t := range types {
		records = append(records, fmt.Sprintf(
			`<APARTMENTTYPE ID="%d" NAME="%s" SHORTNAME="%s" UPDATEDATE="%s" STARTDATE="%s" ENDDATE="%s" ISACTIVE="true"/>`,
			t.id, esc(t.name), esc(t.short), dataDate, dataDate, endDate))
	}
	return records
}

func roomTypes() []string {
	types := []struct {
		id    int
		name  string
		short string
	}{
		{1, "Жилая комната", "жил.комн."},
		{2, "Комната", "комн."},
	}
	records := make([]string, 0, len(types))
	for _, t := range types {
		records = append(records, fmt.Sprintf(
			`<ROOMTYPE ID="%d" NAME="%s" SHORTNAME="%s" UPDATEDATE="%s" STARTDATE="%s" ENDDATE="%s" ISACTIVE="true"/>`,
			t.id, esc(t.name), esc(t.short), dataDate, dataDate, endDate))
	}
	return records
}

// Типы адресных объектов, используемые в TYPENAME цепочки адресов.
func addrObjTypes() []string {
	types := []struct {
		id    int
		level int
		short string
		name  string
	}{
		{1, levelRegion, "Респ", "Республика"},
		{2, levelRegion, "обл", "Область"},
		{3, levelCity, "г", "Город"},
		{4, levelStreet, "ул", "Улица"},
		{5, levelStreet, "пр-кт", "Проспект"},
		{6, levelStreet, "пер", "Переулок"},
	}
	records := make([]string, 0, len(types))
	for _, t := range types {
		records = append(records, fmt.Sprintf(
			`<ADDRESSOBJECTTYPE ID="%d" LEVEL="%d" SHORTNAME="%s" NAME="%s" UPDATEDATE="%s" STARTDATE="%s" ENDDATE="%s" ISACTIVE="true"/>`,
			t.id, t.level, esc(t.short), esc(t.name), dataDate, dataDate, endDate))
	}
	return records
}

// Все уровни объектов действующей схемы ГАР.
func objectLevels() []string {
	names := []string{
		"Субъект Российской Федерации",
		"Административный район",
		"Муниципальный район",
		"Сельское/городское поселение",
		"Город",
		"Населенный пункт",
		"Элемент планировочной структуры",
		"Элемент улично-дорожной сети",
		"Земельный участок",
		"Здание (строение), сооружение",
		"Помещение",
		"Помещение в пределах помещения",
		"Уровень в помещении",
		"Уровень в здании (сооружении)",
		"Рабочий участок",
		"Рабочий элемент",
		"Машино-место",
	}
	records := make([]string, 0, len(names))
	for i, name := range names {
		records = append(records, fmt.Sprintf(
			`<OBJECTLEVEL LEVEL="%d" NAME="%s" UPDATEDATE="%s" STARTDATE="%s" ENDDATE="%s" ISACTIVE="true"/>`,
			i+1, esc(name), dataDate, dataDate, endDate))
	}
	return records
}

// Типы операций: OPERTYPEID записей данных ссылается на присвоение адреса.
func operationTypes() []string {
	types := []struct {
		id    int
		name  string
		short string
	}{
		{10, "Присвоение адреса", "присв."},
		{20, "Изменение реквизитов", "изм."},
	}
	records := make([]string, 0, len(types))
	for _, t := range types {
		records = append(records, fmt.Sprintf(
			`<OPERATIONTYPE ID="%d" NAME="%s" SHORTNAME="%s" UPDATEDATE="%s" STARTDATE="%s" ENDDATE="%s" ISACTIVE="true"/>`,
			t.id, esc(t.name), esc(t.short), dataDate, dataDate, endDate))
	}
	return records
}

const operTypeID = 10 // присвоение адреса

func paramTypes() []string {
	types := []struct {
		id   int
		name string
		code string
	}{
		{1, "Почтовый индекс", "POSTAL_CODE"},
		{2, "ОКТМО", "OKTMO"},
	}
	records := make([]string, 0, len(types))
	for _, t := range types {
		records = append(records, fmt.Sprintf(
			`<PARAMTYPE ID="%d" NAME="%s" CODE="%s" UPDATEDATE="%s" STARTDATE="%s" ENDDATE="%s" ISACTIVE="true"/>`,
			t.id, esc(t.name), t.code, dataDate, dataDate, endDate))
	}
	return records
}

func normativeDocKinds() []string {
	return []string{
		`<NDOCKIND ID="1" NAME="Нормативный правовой акт"/>`,
		`<NDOCKIND ID="2" NAME="Правовой акт органа местного самоуправления"/>`,
	}
}

func normativeDocTypes() []string {
	return []string{
		fmt.Sprintf(`<NDOCTYPE ID="1" NAME="Закон субъекта Российской Федерации" STARTDATE="%s" ENDDATE="%s"/>`, dataDate, endDate),
		fmt.Sprintf(`<NDOCTYPE ID="2" NAME="Постановление" STARTDATE="%s" ENDDATE="%s"/>`, dataDate, endDate),
	}
}

// Цепочка адресообразующих элементов региона.
func addrObjRecords() []string {
	records := make([]string, 0, len(addrObjects))
	for _, o := range addrObjects {
		records = append(records, fmt.Sprintf(
			`<OBJECT ID="%d" OBJECTID="%d" OBJECTGUID="%s" CHANGEID="%d" NAME="%s" TYPENAME="%s" LEVEL="%s" OPERTYPEID="%d" UPDATEDATE="%s" STARTDATE="%s" ENDDATE="%s" ISACTUAL="1" ISACTIVE="1"/>`,
			o.id, o.id, objectGUID(o.id), changeID, esc(o.name), esc(o.typ), o.level, operTypeID, dataDate, dataDate, endDate))
	}
	return records
}

// Иерархия: по записи на каждый объект фикстуры. PATH — точечный путь
// OBJECTID предков, включая сам объект.
func admHierarchy(regionCode string) []string {
	type item struct {
		id     int64
		parent int64
		path   string
	}
	items := make([]item, 0, len(addrObjects)+len(houses)+len(apartments)+len(rooms)+2)
	for _, o := range addrObjects {
		items = append(items, item{o.id, o.parent, o.path})
	}
	for _, h := range houses {
		items = append(items, item{h.id, h.street, h.path})
	}
	for _, a := range apartments {
		items = append(items, item{a.id, a.house, a.path})
	}
	for _, r := range rooms {
		items = append(items, item{r.id, r.apt, r.path})
	}
	items = append(items, item{steadID, 102, steadPath})
	items = append(items, item{carplaceID, 203, carplacePath})

	records := make([]string, 0, len(items))
	for _, it := range items {
		parent := ""
		if it.parent != 0 {
			parent = fmt.Sprintf(` PARENTOBJID="%d"`, it.parent)
		}
		records = append(records, fmt.Sprintf(
			`<ITEM ID="%d" OBJECTID="%d"%s CHANGEID="%d" REGIONCODE="%s" UPDATEDATE="%s" STARTDATE="%s" ENDDATE="%s" ISACTIVE="1" PATH="%s"/>`,
			it.id, it.id, parent, changeID, regionCode, dataDate, dataDate, endDate, it.path))
	}
	return records
}

func houseRecords() []string {
	records := make([]string, 0, len(houses))
	for _, h := range houses {
		add := ""
		if h.addNum1 != "" {
			add = fmt.Sprintf(` ADDNUM1="%s" ADDTYPE1="%d"`, esc(h.addNum1), h.addType1)
		}
		records = append(records, fmt.Sprintf(
			`<HOUSE ID="%d" OBJECTID="%d" OBJECTGUID="%s" CHANGEID="%d" HOUSENUM="%s"%s HOUSETYPE="%d" OPERTYPEID="%d" UPDATEDATE="%s" STARTDATE="%s" ENDDATE="%s" ISACTUAL="1" ISACTIVE="1"/>`,
			h.id, h.id, objectGUID(h.id), changeID, esc(h.num), add, h.houseTyp, operTypeID, dataDate, dataDate, endDate))
	}
	return records
}

func apartmentRecords() []string {
	records := make([]string, 0, len(apartments))
	for _, a := range apartments {
		records = append(records, fmt.Sprintf(
			`<APARTMENT ID="%d" OBJECTID="%d" OBJECTGUID="%s" CHANGEID="%d" NUMBER="%s" APARTTYPE="%d" OPERTYPEID="%d" UPDATEDATE="%s" STARTDATE="%s" ENDDATE="%s" ISACTUAL="1" ISACTIVE="1"/>`,
			a.id, a.id, objectGUID(a.id), changeID, esc(a.num), a.typ, operTypeID, dataDate, dataDate, endDate))
	}
	return records
}

func roomRecords() []string {
	records := make([]string, 0, len(rooms))
	for _, r := range rooms {
		records = append(records, fmt.Sprintf(
			`<ROOM ID="%d" OBJECTID="%d" OBJECTGUID="%s" CHANGEID="%d" NUMBER="%s" ROOMTYPE="%d" OPERTYPEID="%d" UPDATEDATE="%s" STARTDATE="%s" ENDDATE="%s" ISACTUAL="1" ISACTIVE="1"/>`,
			r.id, r.id, objectGUID(r.id), changeID, esc(r.num), r.typ, operTypeID, dataDate, dataDate, endDate))
	}
	return records
}

func steadRecords() []string {
	return []string{fmt.Sprintf(
		`<STEAD ID="%d" OBJECTID="%d" OBJECTGUID="%s" CHANGEID="%d" NUMBER="%s" OPERTYPEID="%d" UPDATEDATE="%s" STARTDATE="%s" ENDDATE="%s" ISACTUAL="1" ISACTIVE="1"/>`,
		steadID, steadID, objectGUID(steadID), changeID, "01:01:000001:1", operTypeID, dataDate, dataDate, endDate)}
}

func carplaceRecords() []string {
	return []string{fmt.Sprintf(
		`<CARPLACE ID="%d" OBJECTID="%d" OBJECTGUID="%s" CHANGEID="%d" NUMBER="%s" OPERTYPEID="%d" UPDATEDATE="%s" STARTDATE="%s" ENDDATE="%s" ISACTUAL="1" ISACTIVE="1"/>`,
		carplaceID, carplaceID, objectGUID(carplaceID), changeID, "М-101", operTypeID, dataDate, dataDate, endDate)}
}

// Реестр объектов: по записи на каждый OBJECTID фикстуры с уровнем
// из AS_OBJECT_LEVELS.
func reestrObjects() []string {
	type entry struct {
		id    int64
		level int
	}
	entries := make([]entry, 0, len(addrObjects)+len(houses)+len(apartments)+len(rooms)+2)
	for _, o := range addrObjects {
		level := levelStreet
		switch o.level {
		case "1":
			level = levelRegion
		case "4":
			level = levelCity
		}
		entries = append(entries, entry{o.id, level})
	}
	for _, h := range houses {
		entries = append(entries, entry{h.id, levelHouse})
	}
	for _, a := range apartments {
		entries = append(entries, entry{a.id, levelApartment})
	}
	for _, r := range rooms {
		entries = append(entries, entry{r.id, levelRoom})
	}
	entries = append(entries, entry{steadID, levelStead}, entry{carplaceID, levelCarplace})

	records := make([]string, 0, len(entries))
	for _, e := range entries {
		records = append(records, fmt.Sprintf(
			`<OBJECT OBJECTID="%d" CREATEDATE="%s" CHANGEID="%d" LEVELID="%d" UPDATEDATE="%s" OBJECTGUID="%s" ISACTIVE="1"/>`,
			e.id, dataDate, changeID, e.level, dataDate, objectGUID(e.id)))
	}
	return records
}

// Параметры адресных объектов: почтовый индекс и ОКТМО для каждой улицы.
func addrObjParams() []string {
	var records []string
	id := 1
	for _, o := range addrObjects {
		if o.level != "5" {
			continue
		}
		for _, p := range []struct {
			typeID int
			value  string
		}{
			{1, "385000"},
			{2, "79605000000"},
		} {
			records = append(records, fmt.Sprintf(
				`<PARAM ID="%d" OBJECTID="%d" CHANGEID="%d" CHANGEIDEND="%d" TYPEID="%d" VALUE="%s" UPDATEDATE="%s" STARTDATE="%s" ENDDATE="%s"/>`,
				id, o.id, changeID, changeID, p.typeID, p.value, dataDate, dataDate, endDate))
			id++
		}
	}
	return records
}
