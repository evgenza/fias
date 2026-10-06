package model

import (
	"encoding/xml"
	"testing"
	"time"
)

// Примеры записей повторяют структуру атрибутов из XSD-схем выгрузки;
// значения — из «Правил формирования адресной строки» (пример адреса
// в городском округе Павлово-Посадский).

func TestAddrObjXML(t *testing.T) {
	src := `<OBJECT ID="1087892" OBJECTID="162142236" OBJECTGUID="ab08d8da-ca9c-4a36-953a-6f1ce0d5b28e" CHANGEID="1532919" NAME="Павлово-Посадский" TYPENAME="г.о." LEVEL="3" OPERTYPEID="10" UPDATEDATE="2024-11-23" STARTDATE="1900-01-01" ENDDATE="2079-06-06" ISACTUAL="1" ISACTIVE="1"/>`

	var got AddrObj
	if err := xml.Unmarshal([]byte(src), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != 1087892 || got.ObjectID != 162142236 {
		t.Errorf("ids = %d/%d", got.ID, got.ObjectID)
	}
	if got.Name != "Павлово-Посадский" || got.TypeName != "г.о." || got.Level != 3 {
		t.Errorf("name/type/level = %q/%q/%d", got.Name, got.TypeName, got.Level)
	}
	if got.StartDate != (Date{1900, time.January, 1}) || got.EndDate != (Date{2079, time.June, 6}) {
		t.Errorf("dates = %v..%v", got.StartDate, got.EndDate)
	}
	if !got.IsActual || !got.IsActive {
		t.Errorf("flags = %v/%v", got.IsActual, got.IsActive)
	}
	if got.PrevID != nil || got.NextID != nil {
		t.Errorf("optional prev/next must be nil when absent")
	}
}

func TestHouseXML(t *testing.T) {
	src := `<HOUSE ID="69095652" OBJECTID="44870981" OBJECTGUID="2d1d8bf6-2b6c-4a3e-9f9e-1d0f2c1e0a11" CHANGEID="1532919" HOUSENUM="93" HOUSETYPE="2" OPERTYPEID="10" UPDATEDATE="2024-11-23" STARTDATE="1900-01-01" ENDDATE="2079-06-06" ISACTUAL="1" ISACTIVE="1"/>`

	var got House
	if err := xml.Unmarshal([]byte(src), &got); err != nil {
		t.Fatal(err)
	}
	if got.HouseNum == nil || *got.HouseNum != "93" {
		t.Errorf("house_num = %v", got.HouseNum)
	}
	if got.HouseType == nil || *got.HouseType != 2 {
		t.Errorf("house_type = %v", got.HouseType)
	}
	if got.AddNum1 != nil || got.AddNum2 != nil || got.AddType1 != nil || got.AddType2 != nil {
		t.Errorf("optional add fields must be nil when absent")
	}
}

func TestAdmHierarchyXML(t *testing.T) {
	src := `<ITEM ID="231648384" OBJECTID="44877013" PARENTOBJID="44876904" CHANGEID="1532919" REGIONCODE="50" AREACODE="000" CITYCODE="000" PLACECODE="000" PLANCODE="000" STREETCODE="000" UPDATEDATE="2024-11-23" STARTDATE="1900-01-01" ENDDATE="2079-06-06" ISACTIVE="1" PATH="807356.162142236.815937.828325.44870981.44876904.44877013"/>`

	var got AdmHierarchyItem
	if err := xml.Unmarshal([]byte(src), &got); err != nil {
		t.Fatal(err)
	}
	if got.ParentObjID == nil || *got.ParentObjID != 44876904 {
		t.Errorf("parent = %v", got.ParentObjID)
	}
	if got.Path != "807356.162142236.815937.828325.44870981.44876904.44877013" {
		t.Errorf("path = %q", got.Path)
	}
	if got.RegionCode == nil || *got.RegionCode != "50" {
		t.Errorf("region_code = %v", got.RegionCode)
	}
	if !got.IsActive {
		t.Error("is_active must be true")
	}
}

func TestReestrObjectXML(t *testing.T) {
	src := `<OBJECT OBJECTID="44877013" CREATEDATE="2015-01-01" CHANGEID="1532919" LEVELID="12" UPDATEDATE="2024-11-23" OBJECTGUID="c9b9b6e6-6f2a-4b0e-8f4f-6e3f1a5e6b99" ISACTIVE="1"/>`

	var got ReestrObject
	if err := xml.Unmarshal([]byte(src), &got); err != nil {
		t.Fatal(err)
	}
	if got.ObjectID != 44877013 || got.LevelID != 12 {
		t.Errorf("object/level = %d/%d", got.ObjectID, got.LevelID)
	}
	if got.CreateDate != (Date{2015, time.January, 1}) {
		t.Errorf("create_date = %v", got.CreateDate)
	}
	if !got.IsActive {
		t.Error("is_active must be true")
	}
}

func TestAddrObjTypeXML(t *testing.T) {
	// В справочниках ISACTIVE — xs:boolean.
	src := `<ADDRESSOBJECTTYPE ID="414" LEVEL="3" SHORTNAME="г.о" NAME="Городской округ" UPDATEDATE="2024-11-23" STARTDATE="1900-01-01" ENDDATE="2079-06-06" ISACTIVE="true"/>`

	var got AddrObjType
	if err := xml.Unmarshal([]byte(src), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != 414 || got.Level != 3 || got.ShortName != "г.о" || got.Name != "Городской округ" {
		t.Errorf("got %+v", got)
	}
	if !got.IsActive {
		t.Error("is_active must be true")
	}
	if got.Descr != nil {
		t.Error("descr must be nil when absent")
	}
}
