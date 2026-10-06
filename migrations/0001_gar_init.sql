-- Схема данных ГАР для загрузки файловой выгрузки и последующего поиска.
-- Таблицы соответствуют 22 форматам выгрузки (см. ../.., каталог unpacked/xsd)
-- и моделям internal/model.
--
-- Внешние ключи намеренно не создаются: таблицы заполняются массовой
-- загрузкой (COPY), а ссылки проверяются на этапе разбора.

CREATE SCHEMA IF NOT EXISTS gar;

-- Расширение для нечёткого поиска по наименованиям.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- ---------------------------------------------------------------------------
-- Объекты адресации и адресообразующие элементы
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS gar.addr_obj (
    id          bigint      PRIMARY KEY,
    object_id   bigint      NOT NULL,
    object_guid uuid        NOT NULL,
    change_id   bigint      NOT NULL,
    name        text        NOT NULL,
    type_name   text        NOT NULL,
    level       smallint    NOT NULL,
    opertype_id integer     NOT NULL,
    prev_id     bigint,
    next_id     bigint,
    update_date date        NOT NULL,
    start_date  date        NOT NULL,
    end_date    date        NOT NULL,
    is_actual   boolean     NOT NULL,
    is_active   boolean     NOT NULL
);

CREATE INDEX IF NOT EXISTS addr_obj_object_id_idx ON gar.addr_obj (object_id);
CREATE INDEX IF NOT EXISTS addr_obj_level_active_idx ON gar.addr_obj (level) WHERE is_actual AND is_active;
CREATE INDEX IF NOT EXISTS addr_obj_name_trgm_idx ON gar.addr_obj USING gin (name gin_trgm_ops);

CREATE TABLE IF NOT EXISTS gar.houses (
    id          bigint      PRIMARY KEY,
    object_id   bigint      NOT NULL,
    object_guid uuid        NOT NULL,
    change_id   bigint      NOT NULL,
    house_num   text,
    add_num1    text,
    add_num2    text,
    house_type  integer,
    add_type1   integer,
    add_type2   integer,
    opertype_id integer     NOT NULL,
    prev_id     bigint,
    next_id     bigint,
    update_date date        NOT NULL,
    start_date  date        NOT NULL,
    end_date    date        NOT NULL,
    is_actual   boolean     NOT NULL,
    is_active   boolean     NOT NULL
);

CREATE INDEX IF NOT EXISTS houses_object_id_idx ON gar.houses (object_id);
CREATE INDEX IF NOT EXISTS houses_num_trgm_idx ON gar.houses USING gin (house_num gin_trgm_ops);

CREATE TABLE IF NOT EXISTS gar.steads (
    id          bigint      PRIMARY KEY,
    object_id   bigint      NOT NULL,
    object_guid uuid        NOT NULL,
    change_id   bigint      NOT NULL,
    number      text        NOT NULL,
    opertype_id integer     NOT NULL,
    prev_id     bigint,
    next_id     bigint,
    update_date date        NOT NULL,
    start_date  date        NOT NULL,
    end_date    date        NOT NULL,
    is_actual   boolean     NOT NULL,
    is_active   boolean     NOT NULL
);

CREATE INDEX IF NOT EXISTS steads_object_id_idx ON gar.steads (object_id);

CREATE TABLE IF NOT EXISTS gar.apartments (
    id          bigint      PRIMARY KEY,
    object_id   bigint      NOT NULL,
    object_guid uuid        NOT NULL,
    change_id   bigint      NOT NULL,
    number      text        NOT NULL,
    apart_type  integer     NOT NULL,
    opertype_id integer     NOT NULL,
    prev_id     bigint,
    next_id     bigint,
    update_date date        NOT NULL,
    start_date  date        NOT NULL,
    end_date    date        NOT NULL,
    is_actual   boolean     NOT NULL,
    is_active   boolean     NOT NULL
);

CREATE INDEX IF NOT EXISTS apartments_object_id_idx ON gar.apartments (object_id);

CREATE TABLE IF NOT EXISTS gar.rooms (
    id          bigint      PRIMARY KEY,
    object_id   bigint      NOT NULL,
    object_guid uuid        NOT NULL,
    change_id   bigint      NOT NULL,
    number      text        NOT NULL,
    room_type   integer     NOT NULL,
    opertype_id integer     NOT NULL,
    prev_id     bigint,
    next_id     bigint,
    update_date date        NOT NULL,
    start_date  date        NOT NULL,
    end_date    date        NOT NULL,
    is_actual   boolean     NOT NULL,
    is_active   boolean     NOT NULL
);

CREATE INDEX IF NOT EXISTS rooms_object_id_idx ON gar.rooms (object_id);

CREATE TABLE IF NOT EXISTS gar.carplaces (
    id          bigint      PRIMARY KEY,
    object_id   bigint      NOT NULL,
    object_guid uuid        NOT NULL,
    change_id   bigint      NOT NULL,
    number      text        NOT NULL,
    opertype_id integer     NOT NULL,
    prev_id     bigint,
    next_id     bigint,
    update_date date        NOT NULL,
    start_date  date        NOT NULL,
    end_date    date        NOT NULL,
    is_actual   boolean     NOT NULL,
    is_active   boolean     NOT NULL
);

CREATE INDEX IF NOT EXISTS carplaces_object_id_idx ON gar.carplaces (object_id);

-- Реестр объектов: OBJECTID ↔ OBJECTGUID, уровень и действительность.
CREATE TABLE IF NOT EXISTS gar.reestr_objects (
    object_id   bigint      PRIMARY KEY,
    create_date date        NOT NULL,
    change_id   bigint      NOT NULL,
    level_id    integer     NOT NULL,
    update_date date        NOT NULL,
    object_guid uuid        NOT NULL,
    is_active   boolean     NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS reestr_objects_guid_idx ON gar.reestr_objects (object_guid);

-- ---------------------------------------------------------------------------
-- Иерархии и изменения
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS gar.adm_hierarchy (
    id            bigint      PRIMARY KEY,
    object_id     bigint      NOT NULL,
    parent_obj_id bigint,
    change_id     bigint      NOT NULL,
    region_code   varchar(4),
    area_code     varchar(4),
    city_code     varchar(4),
    place_code    varchar(4),
    plan_code     varchar(4),
    street_code   varchar(4),
    prev_id       bigint,
    next_id       bigint,
    update_date   date        NOT NULL,
    start_date    date        NOT NULL,
    end_date      date        NOT NULL,
    is_active     boolean     NOT NULL,
    path          text        NOT NULL
);

CREATE INDEX IF NOT EXISTS adm_hierarchy_object_id_idx ON gar.adm_hierarchy (object_id);
CREATE INDEX IF NOT EXISTS adm_hierarchy_parent_idx ON gar.adm_hierarchy (parent_obj_id);

CREATE TABLE IF NOT EXISTS gar.mun_hierarchy (
    id            bigint      PRIMARY KEY,
    object_id     bigint      NOT NULL,
    parent_obj_id bigint,
    change_id     bigint      NOT NULL,
    oktmo         varchar(11),
    prev_id       bigint,
    next_id       bigint,
    update_date   date        NOT NULL,
    start_date    date        NOT NULL,
    end_date      date        NOT NULL,
    is_active     boolean     NOT NULL,
    path          text        NOT NULL
);

CREATE INDEX IF NOT EXISTS mun_hierarchy_object_id_idx ON gar.mun_hierarchy (object_id);
CREATE INDEX IF NOT EXISTS mun_hierarchy_parent_idx ON gar.mun_hierarchy (parent_obj_id);

CREATE TABLE IF NOT EXISTS gar.addr_obj_division (
    id        bigint PRIMARY KEY,
    parent_id bigint NOT NULL,
    child_id  bigint NOT NULL,
    change_id bigint NOT NULL
);

CREATE INDEX IF NOT EXISTS addr_obj_division_child_idx ON gar.addr_obj_division (child_id);
CREATE INDEX IF NOT EXISTS addr_obj_division_parent_idx ON gar.addr_obj_division (parent_id);

CREATE TABLE IF NOT EXISTS gar.change_history (
    change_id    bigint   PRIMARY KEY,
    object_id    bigint   NOT NULL,
    adr_object_id bigint  NOT NULL,
    opertype_id  integer  NOT NULL,
    ndoc_id      bigint,
    change_date  date     NOT NULL
);

CREATE INDEX IF NOT EXISTS change_history_object_id_idx ON gar.change_history (object_id);

-- ---------------------------------------------------------------------------
-- Параметры и справочники
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS gar.param (
    id            bigint      PRIMARY KEY,
    object_id     bigint      NOT NULL,
    change_id     bigint,
    change_id_end bigint      NOT NULL,
    type_id       integer     NOT NULL,
    value         text        NOT NULL,
    update_date   date        NOT NULL,
    start_date    date        NOT NULL,
    end_date      date        NOT NULL
);

CREATE INDEX IF NOT EXISTS param_object_type_idx ON gar.param (object_id, type_id);

CREATE TABLE IF NOT EXISTS gar.addr_obj_types (
    id          integer     PRIMARY KEY,
    level       smallint    NOT NULL,
    shortname   text        NOT NULL,
    name        text        NOT NULL,
    descr       text,
    update_date date        NOT NULL,
    start_date  date        NOT NULL,
    end_date    date        NOT NULL,
    is_active   boolean     NOT NULL
);

CREATE INDEX IF NOT EXISTS addr_obj_types_level_idx ON gar.addr_obj_types (level, shortname);

CREATE TABLE IF NOT EXISTS gar.apartment_types (
    id          integer     PRIMARY KEY,
    name        text        NOT NULL,
    shortname   text,
    descr       text,
    update_date date        NOT NULL,
    start_date  date        NOT NULL,
    end_date    date        NOT NULL,
    is_active   boolean     NOT NULL
);

CREATE TABLE IF NOT EXISTS gar.house_types (
    id          integer     PRIMARY KEY,
    name        text        NOT NULL,
    shortname   text,
    descr       text,
    update_date date        NOT NULL,
    start_date  date        NOT NULL,
    end_date    date        NOT NULL,
    is_active   boolean     NOT NULL
);

CREATE TABLE IF NOT EXISTS gar.room_types (
    id          integer     PRIMARY KEY,
    name        text        NOT NULL,
    shortname   text,
    descr       text,
    update_date date        NOT NULL,
    start_date  date        NOT NULL,
    end_date    date        NOT NULL,
    is_active   boolean     NOT NULL
);

CREATE TABLE IF NOT EXISTS gar.operation_types (
    id          integer     PRIMARY KEY,
    name        text        NOT NULL,
    shortname   text,
    descr       text,
    update_date date        NOT NULL,
    start_date  date        NOT NULL,
    end_date    date        NOT NULL,
    is_active   boolean     NOT NULL
);

CREATE TABLE IF NOT EXISTS gar.param_types (
    id          integer     PRIMARY KEY,
    name        text        NOT NULL,
    code        text        NOT NULL,
    descr       text,
    update_date date        NOT NULL,
    start_date  date        NOT NULL,
    end_date    date        NOT NULL,
    is_active   boolean     NOT NULL
);

CREATE TABLE IF NOT EXISTS gar.object_levels (
    level       smallint    PRIMARY KEY,
    name        text        NOT NULL,
    shortname   text,
    update_date date        NOT NULL,
    start_date  date        NOT NULL,
    end_date    date        NOT NULL,
    is_active   boolean     NOT NULL
);

CREATE TABLE IF NOT EXISTS gar.normative_docs (
    id          bigint      PRIMARY KEY,
    name        text        NOT NULL,
    doc_date    date        NOT NULL,
    number      text        NOT NULL,
    type        integer     NOT NULL,
    kind        integer     NOT NULL,
    update_date date        NOT NULL,
    orgname     text,
    regnum      text,
    reg_date    date,
    acc_date    date,
    comment     text
);

CREATE TABLE IF NOT EXISTS gar.normative_doc_kinds (
    id   integer PRIMARY KEY,
    name text    NOT NULL
);

CREATE TABLE IF NOT EXISTS gar.normative_doc_types (
    id         integer PRIMARY KEY,
    name       text    NOT NULL,
    start_date date    NOT NULL,
    end_date   date    NOT NULL
);
