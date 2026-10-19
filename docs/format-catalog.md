# Каталог форматов выгрузки ГАР

Сгенерирован из исходных XSD (`xsd/` в репозитории), описаний Ent-схем
(`ent describe`) и каталога `internal/gar` при базовом анализе (этап 0,
октябрь 2026). Источник истины по обязательности атрибутов — XSD; колонки SQL
определяет `ent/schema`.

## Семейства форматов (23 Kind)

| Kind | Ent-схема | SQL-таблица | root | запись | ключ | обязательные атрибуты | optional-атрибуты |
|---|---|---|---|---|---|---|---|
| AS_ADDR_OBJ | AddrObj | addr_obj | ADDRESSOBJECTS | OBJECT | ID | ID (int64); OBJECTID (int64); OBJECTGUID (string); CHANGEID (int64); NAME (string); TYPENAME (string); LEVEL (string); OPERTYPEID (Integer); UPDATEDATE (Date); STARTDATE (Date); ENDDATE (Date); ISACTUAL (Flag); ISACTIVE (Flag) | PREVID (int64); NEXTID (int64) |
| AS_ADDR_OBJ_DIVISION | AddrObjDivision | addr_obj_division | ITEMS | ITEM | ID | ID (int64); PARENTID (int64); CHILDID (int64); CHANGEID (int64) | — |
| AS_ADDR_OBJ_TYPES | AddrObjType | addr_obj_types | ADDRESSOBJECTTYPES | ADDRESSOBJECTTYPE | ID | ID (int64); LEVEL (Integer); SHORTNAME (string); NAME (string); UPDATEDATE (Date); STARTDATE (Date); ENDDATE (Date); ISACTIVE (Flag) | DESC (string) |
| AS_ADM_HIERARCHY | AdmHierarchyItem | adm_hierarchy | ITEMS | ITEM | ID | ID (int64); OBJECTID (int64); CHANGEID (int64); UPDATEDATE (Date); STARTDATE (Date); ENDDATE (Date); ISACTIVE (Flag); PATH (string) | PARENTOBJID (int64); REGIONCODE (string); AREACODE (string); CITYCODE (string); PLACECODE (string); PLANCODE (string); STREETCODE (string); PREVID (int64); NEXTID (int64) |
| AS_APARTMENTS | Apartment | apartments | APARTMENTS | APARTMENT | ID | ID (int64); OBJECTID (int64); OBJECTGUID (string); CHANGEID (int64); NUMBER (string); APARTTYPE (int64); OPERTYPEID (int64); UPDATEDATE (Date); STARTDATE (Date); ENDDATE (Date); ISACTUAL (Flag); ISACTIVE (Flag) | PREVID (int64); NEXTID (int64) |
| AS_APARTMENT_TYPES | ApartmentType | apartment_types | APARTMENTTYPES | APARTMENTTYPE | ID | ID (int64); NAME (string); UPDATEDATE (Date); STARTDATE (Date); ENDDATE (Date); ISACTIVE (Flag) | SHORTNAME (string); DESC (string) |
| AS_CARPLACES | Carplace | carplaces | CARPLACES | CARPLACE | ID | ID (int64); OBJECTID (int64); OBJECTGUID (string); CHANGEID (int64); NUMBER (string); OPERTYPEID (int64); UPDATEDATE (Date); STARTDATE (Date); ENDDATE (Date); ISACTUAL (Flag); ISACTIVE (Flag) | PREVID (int64); NEXTID (int64) |
| AS_CHANGE_HISTORY | ChangeHistory | change_history | ITEMS | ITEM | CHANGEID (колонка change_id) | CHANGEID (int64); OBJECTID (int64); ADROBJECTID (string); OPERTYPEID (int64); CHANGEDATE (Date) | NDOCID (int64) |
| AS_HOUSES | House | houses | HOUSES | HOUSE | ID | ID (int64); OBJECTID (int64); OBJECTGUID (string); CHANGEID (int64); OPERTYPEID (Integer); UPDATEDATE (Date); STARTDATE (Date); ENDDATE (Date); ISACTUAL (Flag); ISACTIVE (Flag) | HOUSENUM (string); ADDNUM1 (string); ADDNUM2 (string); HOUSETYPE (Integer); ADDTYPE1 (Integer); ADDTYPE2 (Integer); PREVID (int64); NEXTID (int64) |
| AS_HOUSE_TYPES | HouseType | house_types | HOUSETYPES | HOUSETYPE | ID | ID (int64); NAME (string); UPDATEDATE (Date); STARTDATE (Date); ENDDATE (Date); ISACTIVE (Flag) | SHORTNAME (string); DESC (string) |
| AS_MUN_HIERARCHY | MunHierarchyItem | mun_hierarchy | ITEMS | ITEM | ID | ID (int64); OBJECTID (int64); CHANGEID (int64); UPDATEDATE (Date); STARTDATE (Date); ENDDATE (Date); ISACTIVE (Flag); PATH (string) | PARENTOBJID (int64); OKTMO (string); PREVID (int64); NEXTID (int64) |
| AS_NORMATIVE_DOCS | NormativeDoc | normative_docs | NORMDOCS | NORMDOC | ID | ID (int64); NAME (string); DATE (Date); NUMBER (string); TYPE (int64); KIND (int64); UPDATEDATE (Date) | ORGNAME (string); REGNUM (string); REGDATE (Date); ACCDATE (Date); COMMENT (string) |
| AS_NORMATIVE_DOCS_KINDS | NormativeDocKind | normative_doc_kinds | NDOCKINDS | NDOCKIND | ID (numeric) | ID (Integer); NAME (string) | — |
| AS_NORMATIVE_DOCS_TYPES | NormativeDocType | normative_doc_types | NDOCTYPES | NDOCTYPE | ID (numeric) | ID (Integer); NAME (string); STARTDATE (Date); ENDDATE (Date) | — |
| AS_OBJECT_LEVELS | ObjectLevel | object_levels | OBJECTLEVELS | OBJECTLEVEL | LEVEL (колонка level) | LEVEL (int64); NAME (string); UPDATEDATE (Date); STARTDATE (Date); ENDDATE (Date); ISACTIVE (Flag) | SHORTNAME (string) |
| AS_OPERATION_TYPES | OperationType | operation_types | OPERATIONTYPES | OPERATIONTYPE | ID (numeric) | ID (Integer); NAME (string); UPDATEDATE (Date); STARTDATE (Date); ENDDATE (Date); ISACTIVE (Flag) | SHORTNAME (string); DESC (string) |
| AS_PARAM | Param | param | PARAMS | PARAM | (source, source_id) + суррогатный id | ID (int64); OBJECTID (int64); CHANGEIDEND (int64); TYPEID (int64); VALUE (string); UPDATEDATE (Date); STARTDATE (Date); ENDDATE (Date) | CHANGEID (int64) |
| AS_PARAM_TYPES | ParamType | param_types | PARAMTYPES | PARAMTYPE | ID | ID (int64); NAME (string); CODE (string); UPDATEDATE (Date); STARTDATE (Date); ENDDATE (Date); ISACTIVE (Flag) | DESC (string) |
| AS_REESTR_OBJECTS | ReestrObject | reestr_objects | REESTR_OBJECTS | OBJECT | OBJECTID (колонка object_id) | OBJECTID (int64); CREATEDATE (Date); CHANGEID (int64); LEVELID (int64); UPDATEDATE (Date); OBJECTGUID (string); ISACTIVE (Flag) | — |
| AS_ROOMS | Room | rooms | ROOMS | ROOM | ID | ID (int64); OBJECTID (int64); OBJECTGUID (string); CHANGEID (int64); NUMBER (string); ROOMTYPE (int64); OPERTYPEID (int64); UPDATEDATE (Date); STARTDATE (Date); ENDDATE (Date); ISACTUAL (Flag); ISACTIVE (Flag) | PREVID (int64); NEXTID (int64) |
| AS_ROOM_TYPES | RoomType | room_types | ROOMTYPES | ROOMTYPE | ID | ID (int64); NAME (string); UPDATEDATE (Date); STARTDATE (Date); ENDDATE (Date); ISACTIVE (Flag) | SHORTNAME (string); DESC (string) |
| AS_STEADS | Stead | steads | STEADS | STEAD | ID | ID (Integer); OBJECTID (Integer); OBJECTGUID (string); CHANGEID (Integer); NUMBER (string); OPERTYPEID (string); UPDATEDATE (Date); STARTDATE (Date); ENDDATE (Date); ISACTUAL (Flag); ISACTIVE (Flag) | PREVID (Integer); NEXTID (Integer) |
| AS_ADDHOUSE_TYPES | AddHouseType | add_house_types | HOUSETYPES | HOUSETYPE | ID | ID; NAME; UPDATEDATE; STARTDATE; ENDDATE; ISACTIVE | SHORTNAME; DESC |

Примечания к таблице:

- `AS_ADDHOUSE_TYPES` отдельного XSD не имеет: используется XSD `AS_HOUSE_TYPES`
  (тот же root `HOUSETYPES`, запись `HOUSETYPE`); таблица отдельная (`add_house_types`).
- Ключи трёх семейств отличаются от ID записи: `AS_CHANGE_HISTORY` — `CHANGEID`,
  `AS_REESTR_OBJECTS` — `OBJECTID`, `AS_OBJECT_LEVELS` — `LEVEL` (ID записи в XSD нет).
- `AS_NORMATIVE_DOCS_KINDS`, `AS_NORMATIVE_DOCS_TYPES`, `AS_OPERATION_TYPES` имеют
  `ID` типа xs:integer и хранятся как `numeric` (gartype.Integer); `AS_STEADS` — ID как `numeric(19,0)`.
- `LEVEL` адресных объектов — строка с ведущими нулями (`varchar(10)`); коды
  REGIONCODE/AREACODE/… и OKTMO — тоже строки с ведущими нулями.

## Параметры (AS_PARAM)

Файлы шести семейств объектов декодируются в одну таблицу `param` c бизнес-ключом
`(source, source_id)`; нормализация имени файла:

| префикс файла | source |
|---|---|
| AS_ADDR_OBJ_PARAM(S) | AS_ADDR_OBJ_PARAMS |
| AS_HOUSES_PARAM(S) | AS_HOUSES_PARAMS |
| AS_APARTMENTS_PARAM(S) | AS_APARTMENTS_PARAMS |
| AS_ROOMS_PARAM(S) | AS_ROOMS_PARAMS |
| AS_STEADS_PARAM(S) | AS_STEADS_PARAMS |
| AS_CARPLACES_PARAM(S) | AS_CARPLACES_PARAMS |

Обязательные атрибуты PARAM: ID, OBJECTID, TYPEID, VALUE, CHANGEIDEND, UPDATEDATE, STARTDATE, ENDDATE;
optional: CHANGEID. Поле `source` — синтетическое (из имени файла), в XSD его нет.

## Типы значений (internal/gartype)

- `Date` — xs:date, календарный день; строка вида `YYYY-MM-DD` (расширенные годы — с минусом);
  в PostgreSQL `date`; JSON — строка, отсутствие — null.
- `Integer` — строковое целое без ограничения разрядности (OPERTYPEID, LEVEL типов и т.п.);
  в PostgreSQL `numeric`; пустое значение — NULL.
- `Flag` — xs:boolean (`true/false/1/0`); в PostgreSQL `boolean`; JSON — bool.

## Специальные правила формата

- `PATH` иерархий — обязательный, тип в XSD не задан (любая строка).
- `PREVID`/`NEXTID` — необязательные ссылки, могут указывать в другое семейство
  (см. «Формирование адресной строки»), FK между raw-таблицами не создаются.
- Повторная запись идентичного входа обновляет строку по ключу (upsert), без дубликатов;
  дубликаты ключа внутри пакета разрешаются по правилу «последняя запись побеждает».
