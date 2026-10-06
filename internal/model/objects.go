// Package model — структуры записей выгрузки ГАР, по одной на каждый
// из 22 форматов. Поля и обязательность соответствуют XSD-схемам из
// комплекта «Сведения о составе информации ГАР» (см. ../.., каталог
// unpacked/xsd). XML-теги повторяют имена атрибутов выгрузки, поэтому
// записи декодируются напрямую из элементов файла.
package model

// AddrObj — запись AS_ADDR_OBJ: адресообразующий элемент (регион, район,
// город, населённый пункт, улица, элемент планировочной структуры).
// Схема AS_ADDR_OBJ_2_251_01_04_01_01.xsd: корневой элемент ADDRESSOBJECTS,
// записи — OBJECT.
type AddrObj struct {
	ID         int64  `xml:"ID,attr"`
	ObjectID   int64  `xml:"OBJECTID,attr"`
	ObjectGUID string `xml:"OBJECTGUID,attr"`
	ChangeID   int64  `xml:"CHANGEID,attr"`
	Name       string `xml:"NAME,attr"`
	TypeName   string `xml:"TYPENAME,attr"` // краткое наименование типа (сокращение)
	Level      int    `xml:"LEVEL,attr"`    // уровень адресного объекта
	OpertypeID int    `xml:"OPERTYPEID,attr"`
	PrevID     *int64 `xml:"PREVID,attr"`
	NextID     *int64 `xml:"NEXTID,attr"`
	UpdateDate Date   `xml:"UPDATEDATE,attr"`
	StartDate  Date   `xml:"STARTDATE,attr"`
	EndDate    Date   `xml:"ENDDATE,attr"`
	IsActual   Flag   `xml:"ISACTUAL,attr"`
	IsActive   Flag   `xml:"ISACTIVE,attr"`
}

// House — запись AS_HOUSES: здание (строение), сооружение.
// Схема AS_HOUSES_2_251_08_04_01_01.xsd: HOUSES / HOUSE.
type House struct {
	ID         int64   `xml:"ID,attr"`
	ObjectID   int64   `xml:"OBJECTID,attr"`
	ObjectGUID string  `xml:"OBJECTGUID,attr"`
	ChangeID   int64   `xml:"CHANGEID,attr"`
	HouseNum   *string `xml:"HOUSENUM,attr"` // основной номер дома
	AddNum1    *string `xml:"ADDNUM1,attr"`  // дополнительный номер 1 (например, корпус)
	AddNum2    *string `xml:"ADDNUM2,attr"`  // дополнительный номер 2 (например, строение)
	HouseType  *int    `xml:"HOUSETYPE,attr"`
	AddType1   *int    `xml:"ADDTYPE1,attr"`
	AddType2   *int    `xml:"ADDTYPE2,attr"`
	OpertypeID int     `xml:"OPERTYPEID,attr"`
	PrevID     *int64  `xml:"PREVID,attr"`
	NextID     *int64  `xml:"NEXTID,attr"`
	UpdateDate Date    `xml:"UPDATEDATE,attr"`
	StartDate  Date    `xml:"STARTDATE,attr"`
	EndDate    Date    `xml:"ENDDATE,attr"`
	IsActual   Flag    `xml:"ISACTUAL,attr"`
	IsActive   Flag    `xml:"ISACTIVE,attr"`
}

// Stead — запись AS_STEADS: земельный участок.
// Схема AS_STEADS_2_251_18_04_01_01.xsd: STEADS / STEAD.
type Stead struct {
	ID         int64  `xml:"ID,attr"`
	ObjectID   int64  `xml:"OBJECTID,attr"`
	ObjectGUID string `xml:"OBJECTGUID,attr"`
	ChangeID   int64  `xml:"CHANGEID,attr"`
	Number     string `xml:"NUMBER,attr"` // номер земельного участка
	OpertypeID int    `xml:"OPERTYPEID,attr"`
	PrevID     *int64 `xml:"PREVID,attr"`
	NextID     *int64 `xml:"NEXTID,attr"`
	UpdateDate Date   `xml:"UPDATEDATE,attr"`
	StartDate  Date   `xml:"STARTDATE,attr"`
	EndDate    Date   `xml:"ENDDATE,attr"`
	IsActual   Flag   `xml:"ISACTUAL,attr"`
	IsActive   Flag   `xml:"ISACTIVE,attr"`
}

// Apartment — запись AS_APARTMENTS: помещение.
// Схема AS_APARTMENTS_2_251_05_04_01_01.xsd: APARTMENTS / APARTMENT.
type Apartment struct {
	ID         int64  `xml:"ID,attr"`
	ObjectID   int64  `xml:"OBJECTID,attr"`
	ObjectGUID string `xml:"OBJECTGUID,attr"`
	ChangeID   int64  `xml:"CHANGEID,attr"`
	Number     string `xml:"NUMBER,attr"` // номер помещения
	ApartType  int    `xml:"APARTTYPE,attr"`
	OpertypeID int    `xml:"OPERTYPEID,attr"`
	PrevID     *int64 `xml:"PREVID,attr"`
	NextID     *int64 `xml:"NEXTID,attr"`
	UpdateDate Date   `xml:"UPDATEDATE,attr"`
	StartDate  Date   `xml:"STARTDATE,attr"`
	EndDate    Date   `xml:"ENDDATE,attr"`
	IsActual   Flag   `xml:"ISACTUAL,attr"`
	IsActive   Flag   `xml:"ISACTIVE,attr"`
}

// Room — запись AS_ROOMS: помещение в пределах помещения (комната).
// Схема AS_ROOMS_2_251_15_04_01_01.xsd: ROOMS / ROOM.
type Room struct {
	ID         int64  `xml:"ID,attr"`
	ObjectID   int64  `xml:"OBJECTID,attr"`
	ObjectGUID string `xml:"OBJECTGUID,attr"`
	ChangeID   int64  `xml:"CHANGEID,attr"`
	Number     string `xml:"NUMBER,attr"` // номер комнаты или офиса
	RoomType   int    `xml:"ROOMTYPE,attr"`
	OpertypeID int    `xml:"OPERTYPEID,attr"`
	PrevID     *int64 `xml:"PREVID,attr"`
	NextID     *int64 `xml:"NEXTID,attr"`
	UpdateDate Date   `xml:"UPDATEDATE,attr"`
	StartDate  Date   `xml:"STARTDATE,attr"`
	EndDate    Date   `xml:"ENDDATE,attr"`
	IsActual   Flag   `xml:"ISACTUAL,attr"`
	IsActive   Flag   `xml:"ISACTIVE,attr"`
}

// Carplace — запись AS_CARPLACES: машино-место.
// Схема AS_CARPLACES_2_251_06_04_01_01.xsd: CARPLACES / CARPLACE.
type Carplace struct {
	ID         int64  `xml:"ID,attr"`
	ObjectID   int64  `xml:"OBJECTID,attr"`
	ObjectGUID string `xml:"OBJECTGUID,attr"`
	ChangeID   int64  `xml:"CHANGEID,attr"`
	Number     string `xml:"NUMBER,attr"` // номер машино-места
	OpertypeID int    `xml:"OPERTYPEID,attr"`
	PrevID     *int64 `xml:"PREVID,attr"`
	NextID     *int64 `xml:"NEXTID,attr"`
	UpdateDate Date   `xml:"UPDATEDATE,attr"`
	StartDate  Date   `xml:"STARTDATE,attr"`
	EndDate    Date   `xml:"ENDDATE,attr"`
	IsActual   Flag   `xml:"ISACTUAL,attr"`
	IsActive   Flag   `xml:"ISACTIVE,attr"`
}

// ReestrObject — запись AS_REESTR_OBJECTS: реестр объектов, сопоставляющий
// OBJECTID и OBJECTGUID, с уровнем и признаком действительности объекта.
// По одному OBJECTID — одна запись. Схема AS_REESTR_OBJECTS_2_251_22_04_01_01.xsd:
// REESTR_OBJECTS / OBJECT.
type ReestrObject struct {
	ObjectID   int64  `xml:"OBJECTID,attr"`
	CreateDate Date   `xml:"CREATEDATE,attr"`
	ChangeID   int64  `xml:"CHANGEID,attr"`
	LevelID    int    `xml:"LEVELID,attr"`
	UpdateDate Date   `xml:"UPDATEDATE,attr"`
	ObjectGUID string `xml:"OBJECTGUID,attr"`
	IsActive   Flag   `xml:"ISACTIVE,attr"`
}
