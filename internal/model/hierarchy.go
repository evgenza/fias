package model

// Иерархии и служебные таблицы выгрузки ГАР.

// AdmHierarchyItem — запись AS_ADM_HIERARCHY: иерархия в административном
// делении. PATH — материализованный путь: OBJECTID всех предков и самого
// объекта через «.». Схема AS_ADM_HIERARCHY_2_251_04_04_01_01.xsd:
// ITEMS / ITEM.
type AdmHierarchyItem struct {
	ID          int64   `xml:"ID,attr"`
	ObjectID    int64   `xml:"OBJECTID,attr"`
	ParentObjID *int64  `xml:"PARENTOBJID,attr"`
	ChangeID    int64   `xml:"CHANGEID,attr"`
	RegionCode  *string `xml:"REGIONCODE,attr"`
	AreaCode    *string `xml:"AREACODE,attr"`
	CityCode    *string `xml:"CITYCODE,attr"`
	PlaceCode   *string `xml:"PLACECODE,attr"`
	PlanCode    *string `xml:"PLANCODE,attr"`
	StreetCode  *string `xml:"STREETCODE,attr"`
	PrevID      *int64  `xml:"PREVID,attr"`
	NextID      *int64  `xml:"NEXTID,attr"`
	UpdateDate  Date    `xml:"UPDATEDATE,attr"`
	StartDate   Date    `xml:"STARTDATE,attr"`
	EndDate     Date    `xml:"ENDDATE,attr"`
	IsActive    Flag    `xml:"ISACTIVE,attr"`
	Path        string  `xml:"PATH,attr"`
}

// MunHierarchyItem — запись AS_MUN_HIERARCHY: иерархия в муниципальном
// делении. Схема AS_MUN_HIERARCHY_2_251_10_04_01_01.xsd: ITEMS / ITEM.
type MunHierarchyItem struct {
	ID          int64   `xml:"ID,attr"`
	ObjectID    int64   `xml:"OBJECTID,attr"`
	ParentObjID *int64  `xml:"PARENTOBJID,attr"`
	ChangeID    int64   `xml:"CHANGEID,attr"`
	OKTMO       *string `xml:"OKTMO,attr"`
	PrevID      *int64  `xml:"PREVID,attr"`
	NextID      *int64  `xml:"NEXTID,attr"`
	UpdateDate  Date    `xml:"UPDATEDATE,attr"`
	StartDate   Date    `xml:"STARTDATE,attr"`
	EndDate     Date    `xml:"ENDDATE,attr"`
	IsActive    Flag    `xml:"ISACTIVE,attr"`
	Path        string  `xml:"PATH,attr"`
}

// AddrObjDivision — запись AS_ADDR_OBJ_DIVISION: операции переподчинения
// адресных объектов. Схема AS_ADDR_OBJ_DIVISION_2_251_19_04_01_01.xsd:
// ITEMS / ITEM.
type AddrObjDivision struct {
	ID       int64 `xml:"ID,attr"`
	ParentID int64 `xml:"PARENTID,attr"`
	ChildID  int64 `xml:"CHILDID,attr"`
	ChangeID int64 `xml:"CHANGEID,attr"`
}

// ChangeHistory — запись AS_CHANGE_HISTORY: история изменений объектов.
// Схема AS_CHANGE_HISTORY_251_21_04_01_01.xsd: ITEMS / ITEM.
type ChangeHistory struct {
	ChangeID    int64  `xml:"CHANGEID,attr"`
	ObjectID    int64  `xml:"OBJECTID,attr"`
	AdrObjectID int64  `xml:"ADROBJECTID,attr"`
	OpertypeID  int    `xml:"OPERTYPEID,attr"`
	NdocID      *int64 `xml:"NDOCID,attr"`
	ChangeDate  Date   `xml:"CHANGEDATE,attr"`
}
