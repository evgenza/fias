# gar-importer

Загрузчик файловой выгрузки [ГАР (ФИАС)](https://fias.nalog.ru/Frontend) в PostgreSQL
для последующего поиска по адресам.

Документация форматов — соседняя папка `../` (XSD-схемы: `../unpacked/xsd`,
описания полей: `../02 Форматы`, правила построения адресной строки:
`../01 Руководства/Формирование адресной строки.md`).

## Структура

```
cmd/gar-importer   — точка входа (каркас команды)
internal/model     — модели всех 22 форматов выгрузки, XML-теги по XSD
internal/garxml    — типы файлов, разбор имён R_T_GGGGMMDD_N.xml и путей архива
migrations/        — DDL PostgreSQL (схема gar, 22 таблицы, индексы)
```

Внешних зависимостей пока нет — только стандартная библиотека.

## Модели ↔ форматы

| Формат | Модель | Таблица |
|---|---|---|
| `AS_ADDR_OBJ` | `model.AddrObj` | `gar.addr_obj` |
| `AS_HOUSES` | `model.House` | `gar.houses` |
| `AS_STEADS` | `model.Stead` | `gar.steads` |
| `AS_APARTMENTS` | `model.Apartment` | `gar.apartments` |
| `AS_ROOMS` | `model.Room` | `gar.rooms` |
| `AS_CARPLACES` | `model.Carplace` | `gar.carplaces` |
| `AS_REESTR_OBJECTS` | `model.ReestrObject` | `gar.reestr_objects` |
| `AS_ADM_HIERARCHY` | `model.AdmHierarchyItem` | `gar.adm_hierarchy` |
| `AS_MUN_HIERARCHY` | `model.MunHierarchyItem` | `gar.mun_hierarchy` |
| `AS_ADDR_OBJ_DIVISION` | `model.AddrObjDivision` | `gar.addr_obj_division` |
| `AS_CHANGE_HISTORY` | `model.ChangeHistory` | `gar.change_history` |
| `AS_PARAM` | `model.Param` | `gar.param` |
| `AS_ADDR_OBJ_TYPES` | `model.AddrObjType` | `gar.addr_obj_types` |
| `AS_APARTMENT_TYPES` | `model.ApartmentType` | `gar.apartment_types` |
| `AS_HOUSE_TYPES` | `model.HouseType` | `gar.house_types` |
| `AS_ROOM_TYPES` | `model.RoomType` | `gar.room_types` |
| `AS_OPERATION_TYPES` | `model.OperationType` | `gar.operation_types` |
| `AS_PARAM_TYPES` | `model.ParamType` | `gar.param_types` |
| `AS_OBJECT_LEVELS` | `model.ObjectLevel` | `gar.object_levels` |
| `AS_NORMATIVE_DOCS` | `model.NormativeDoc` | `gar.normative_docs` |
| `AS_NORMATIVE_DOCS_KINDS` | `model.NormativeDocKind` | `gar.normative_doc_kinds` |
| `AS_NORMATIVE_DOCS_TYPES` | `model.NormativeDocType` | `gar.normative_doc_types` |

Особенности моделей:

- `model.Date` — календарная дата `xs:date` (YYYY-MM-DD) без времени и
  таймзоны, с поддержкой database/sql (тип `date` в PostgreSQL).
- `model.Flag` — логический признак: в файлах данных это `"1"/"0"`
  (ISACTUAL, ISACTIVE), в справочниках — `"true"/"false"`; оба варианта
  разбираются одинаково и ложатся в `boolean`.
- Необязательные атрибуты XSD — указатели (`*int64`, `*string`, `*Date`).

## Решения по схеме БД

- Идентификаторы записей — `bigint` (19 знаков в XSD), `OBJECTGUID` — `uuid`.
- Внешние ключи не создаются: заполнение через COPY, ссылки проверяются
  на этапе разбора; история (PREVID/NEXTID) остаётся в данных.
- Поиск: `pg_trgm` GIN-индексы на `gar.addr_obj.name` и `gar.houses.house_num`;
  адресная строка строится по `PATH` из иерархий (правила — в документации).
- Дальнейшие шаги: потоковое чтение XML из zip (полная выгрузка ~53 ГБ,
  поэтому без полной загрузки файлов в память), массовая загрузка через
  pgx COPY FROM, затем слой поиска.

## Сборка

```sh
go build ./...
go test ./...
go vet ./...
```
