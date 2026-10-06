package model

// Параметры и справочники выгрузки ГАР.

// Param — запись AS_PARAM: значения параметров адресных элементов
// (например, TYPEID=16 — «Официальное наименование» для уровней 1, 3, 4).
// Схема AS_PARAM_2_251_02_04_01_01.xsd: PARAMS / PARAM.
type Param struct {
	ID          int64  `xml:"ID,attr"`
	ObjectID    int64  `xml:"OBJECTID,attr"`
	ChangeID    *int64 `xml:"CHANGEID,attr"`
	ChangeIDEnd int64  `xml:"CHANGEIDEND,attr"`
	TypeID      int    `xml:"TYPEID,attr"`
	Value       string `xml:"VALUE,attr"` // до 8000 символов
	UpdateDate  Date   `xml:"UPDATEDATE,attr"`
	StartDate   Date   `xml:"STARTDATE,attr"`
	EndDate     Date   `xml:"ENDDATE,attr"`
}

// AddrObjType — запись AS_ADDR_OBJ_TYPES: типы адресных объектов
// (по LEVEL и SHORTNAME). Схема ADDRESSOBJECTTYPES / ADDRESSOBJECTTYPE.
type AddrObjType struct {
	ID         int     `xml:"ID,attr"`
	Level      int     `xml:"LEVEL,attr"`
	ShortName  string  `xml:"SHORTNAME,attr"`
	Name       string  `xml:"NAME,attr"`
	Descr      *string `xml:"DESC,attr"`
	UpdateDate Date    `xml:"UPDATEDATE,attr"`
	StartDate  Date    `xml:"STARTDATE,attr"`
	EndDate    Date    `xml:"ENDDATE,attr"`
	IsActive   Flag    `xml:"ISACTIVE,attr"`
}

// ApartmentType — запись AS_APARTMENT_TYPES: типы помещений.
// Схема APARTMENTTYPES / APARTMENTTYPE.
type ApartmentType struct {
	ID         int     `xml:"ID,attr"`
	Name       string  `xml:"NAME,attr"`
	ShortName  *string `xml:"SHORTNAME,attr"`
	Descr      *string `xml:"DESC,attr"`
	UpdateDate Date    `xml:"UPDATEDATE,attr"`
	StartDate  Date    `xml:"STARTDATE,attr"`
	EndDate    Date    `xml:"ENDDATE,attr"`
	IsActive   Flag    `xml:"ISACTIVE,attr"`
}

// HouseType — запись AS_HOUSE_TYPES: основные и дополнительные типы домов.
// Схема HOUSETYPES / HOUSETYPE.
type HouseType struct {
	ID         int     `xml:"ID,attr"`
	Name       string  `xml:"NAME,attr"`
	ShortName  *string `xml:"SHORTNAME,attr"`
	Descr      *string `xml:"DESC,attr"`
	UpdateDate Date    `xml:"UPDATEDATE,attr"`
	StartDate  Date    `xml:"STARTDATE,attr"`
	EndDate    Date    `xml:"ENDDATE,attr"`
	IsActive   Flag    `xml:"ISACTIVE,attr"`
}

// RoomType — запись AS_ROOM_TYPES: типы комнат.
// Схема ROOMTYPES / ROOMTYPE.
type RoomType struct {
	ID         int     `xml:"ID,attr"`
	Name       string  `xml:"NAME,attr"`
	ShortName  *string `xml:"SHORTNAME,attr"`
	Descr      *string `xml:"DESC,attr"`
	UpdateDate Date    `xml:"UPDATEDATE,attr"`
	StartDate  Date    `xml:"STARTDATE,attr"`
	EndDate    Date    `xml:"ENDDATE,attr"`
	IsActive   Flag    `xml:"ISACTIVE,attr"`
}

// OperationType — запись AS_OPERATION_TYPES: статусы действия над записью
// (значения OPERTYPEID). Схема OPERATIONTYPES / OPERATIONTYPE.
type OperationType struct {
	ID         int     `xml:"ID,attr"`
	Name       string  `xml:"NAME,attr"`
	ShortName  *string `xml:"SHORTNAME,attr"`
	Descr      *string `xml:"DESC,attr"`
	UpdateDate Date    `xml:"UPDATEDATE,attr"`
	StartDate  Date    `xml:"STARTDATE,attr"`
	EndDate    Date    `xml:"ENDDATE,attr"`
	IsActive   Flag    `xml:"ISACTIVE,attr"`
}

// ParamType — запись AS_PARAM_TYPES: типы параметров адресных элементов.
// Схема PARAMTYPES / PARAMTYPE.
type ParamType struct {
	ID         int     `xml:"ID,attr"`
	Name       string  `xml:"NAME,attr"`
	Code       string  `xml:"CODE,attr"`
	Descr      *string `xml:"DESC,attr"`
	UpdateDate Date    `xml:"UPDATEDATE,attr"`
	StartDate  Date    `xml:"STARTDATE,attr"`
	EndDate    Date    `xml:"ENDDATE,attr"`
	IsActive   Flag    `xml:"ISACTIVE,attr"`
}

// ObjectLevel — запись AS_OBJECT_LEVELS: уровни адресных объектов
// (значения LEVEL / LEVELID). Схема OBJECTLEVELS / OBJECTLEVEL.
type ObjectLevel struct {
	Level      int     `xml:"LEVEL,attr"`
	Name       string  `xml:"NAME,attr"`
	ShortName  *string `xml:"SHORTNAME,attr"`
	UpdateDate Date    `xml:"UPDATEDATE,attr"`
	StartDate  Date    `xml:"STARTDATE,attr"`
	EndDate    Date    `xml:"ENDDATE,attr"`
	IsActive   Flag    `xml:"ISACTIVE,attr"`
}

// NormativeDoc — запись AS_NORMATIVE_DOCS: нормативный документ — основание
// присвоения наименования. Схема NORMDOCS / NORMDOC.
type NormativeDoc struct {
	ID         int64   `xml:"ID,attr"`
	Name       string  `xml:"NAME,attr"`
	DocDate    Date    `xml:"DATE,attr"`
	Number     string  `xml:"NUMBER,attr"`
	Type       int     `xml:"TYPE,attr"`
	Kind       int     `xml:"KIND,attr"`
	UpdateDate Date    `xml:"UPDATEDATE,attr"`
	OrgName    *string `xml:"ORGNAME,attr"`
	RegNum     *string `xml:"REGNUM,attr"`
	RegDate    *Date   `xml:"REGDATE,attr"`
	AccDate    *Date   `xml:"ACCDATE,attr"`
	Comment    *string `xml:"COMMENT,attr"`
}

// NormativeDocKind — запись AS_NORMATIVE_DOCS_KINDS: виды нормативных
// документов (значения KIND). Схема NDOCKINDS / NDOCKIND.
type NormativeDocKind struct {
	ID   int    `xml:"ID,attr"`
	Name string `xml:"NAME,attr"`
}

// NormativeDocType — запись AS_NORMATIVE_DOCS_TYPES: типы нормативных
// документов (значения TYPE). Схема NDOCTYPES / NDOCTYPE.
type NormativeDocType struct {
	ID        int    `xml:"ID,attr"`
	Name      string `xml:"NAME,attr"`
	StartDate Date   `xml:"STARTDATE,attr"`
	EndDate   Date   `xml:"ENDDATE,attr"`
}
