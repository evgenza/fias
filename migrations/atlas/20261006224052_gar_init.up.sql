-- create "add_house_types" table
CREATE TABLE "add_house_types" ("id" bigint NOT NULL, "name" text NOT NULL, "shortname" text NULL, "descr" text NULL, "update_date" date NOT NULL, "start_date" date NOT NULL, "end_date" date NOT NULL, "is_active" boolean NOT NULL, PRIMARY KEY ("id"));
-- create "addr_obj" table
CREATE TABLE "addr_obj" ("id" bigint NOT NULL, "object_id" bigint NOT NULL, "object_guid" uuid NOT NULL, "change_id" bigint NOT NULL, "name" text NOT NULL, "type_name" text NOT NULL, "level" character varying(10) NOT NULL, "opertype_id" numeric NOT NULL, "prev_id" bigint NULL, "next_id" bigint NULL, "update_date" date NOT NULL, "start_date" date NOT NULL, "end_date" date NOT NULL, "is_actual" boolean NOT NULL, "is_active" boolean NOT NULL, PRIMARY KEY ("id"));
-- create index "addr_obj_object_id_idx" to table: "addr_obj"
CREATE INDEX "addr_obj_object_id_idx" ON "addr_obj" ("object_id");
-- create index "addr_obj_level_active_idx" to table: "addr_obj"
CREATE INDEX "addr_obj_level_active_idx" ON "addr_obj" ("level") WHERE is_actual AND is_active;
-- create "addr_obj_division" table
CREATE TABLE "addr_obj_division" ("id" bigint NOT NULL, "parent_id" bigint NOT NULL, "child_id" bigint NOT NULL, "change_id" bigint NOT NULL, PRIMARY KEY ("id"));
-- create index "addr_obj_division_parent_idx" to table: "addr_obj_division"
CREATE INDEX "addr_obj_division_parent_idx" ON "addr_obj_division" ("parent_id");
-- create index "addr_obj_division_child_idx" to table: "addr_obj_division"
CREATE INDEX "addr_obj_division_child_idx" ON "addr_obj_division" ("child_id");
-- create "addr_obj_types" table
CREATE TABLE "addr_obj_types" ("id" bigint NOT NULL, "level" numeric NOT NULL, "shortname" text NOT NULL, "name" text NOT NULL, "descr" text NULL, "update_date" date NOT NULL, "start_date" date NOT NULL, "end_date" date NOT NULL, "is_active" boolean NOT NULL, PRIMARY KEY ("id"));
-- create index "addr_obj_types_level_idx" to table: "addr_obj_types"
CREATE INDEX "addr_obj_types_level_idx" ON "addr_obj_types" ("level", "shortname");
-- create "adm_hierarchy" table
CREATE TABLE "adm_hierarchy" ("id" bigint NOT NULL, "object_id" bigint NOT NULL, "parent_obj_id" bigint NULL, "change_id" bigint NOT NULL, "region_code" character varying(4) NULL, "area_code" character varying(4) NULL, "city_code" character varying(4) NULL, "place_code" character varying(4) NULL, "plan_code" character varying(4) NULL, "street_code" character varying(4) NULL, "prev_id" bigint NULL, "next_id" bigint NULL, "update_date" date NOT NULL, "start_date" date NOT NULL, "end_date" date NOT NULL, "is_active" boolean NOT NULL, "path" text NOT NULL, PRIMARY KEY ("id"));
-- create index "adm_hierarchy_object_id_idx" to table: "adm_hierarchy"
CREATE INDEX "adm_hierarchy_object_id_idx" ON "adm_hierarchy" ("object_id");
-- create index "adm_hierarchy_parent_idx" to table: "adm_hierarchy"
CREATE INDEX "adm_hierarchy_parent_idx" ON "adm_hierarchy" ("parent_obj_id");
-- create "apartments" table
CREATE TABLE "apartments" ("id" bigint NOT NULL, "object_id" bigint NOT NULL, "object_guid" uuid NOT NULL, "change_id" bigint NOT NULL, "number" text NOT NULL, "apart_type" bigint NOT NULL, "opertype_id" bigint NOT NULL, "prev_id" bigint NULL, "next_id" bigint NULL, "update_date" date NOT NULL, "start_date" date NOT NULL, "end_date" date NOT NULL, "is_actual" boolean NOT NULL, "is_active" boolean NOT NULL, PRIMARY KEY ("id"));
-- create index "apartments_object_id_idx" to table: "apartments"
CREATE INDEX "apartments_object_id_idx" ON "apartments" ("object_id");
-- create "apartment_types" table
CREATE TABLE "apartment_types" ("id" bigint NOT NULL, "name" text NOT NULL, "shortname" text NULL, "descr" text NULL, "update_date" date NOT NULL, "start_date" date NOT NULL, "end_date" date NOT NULL, "is_active" boolean NOT NULL, PRIMARY KEY ("id"));
-- create "carplaces" table
CREATE TABLE "carplaces" ("id" bigint NOT NULL, "object_id" bigint NOT NULL, "object_guid" uuid NOT NULL, "change_id" bigint NOT NULL, "number" text NOT NULL, "opertype_id" bigint NOT NULL, "prev_id" bigint NULL, "next_id" bigint NULL, "update_date" date NOT NULL, "start_date" date NOT NULL, "end_date" date NOT NULL, "is_actual" boolean NOT NULL, "is_active" boolean NOT NULL, PRIMARY KEY ("id"));
-- create index "carplaces_object_id_idx" to table: "carplaces"
CREATE INDEX "carplaces_object_id_idx" ON "carplaces" ("object_id");
-- create "change_history" table
CREATE TABLE "change_history" ("change_id" bigint NOT NULL, "object_id" bigint NOT NULL, "adr_object_id" uuid NOT NULL, "opertype_id" bigint NOT NULL, "ndoc_id" bigint NULL, "change_date" date NOT NULL, PRIMARY KEY ("change_id"));
-- create index "change_history_object_id_idx" to table: "change_history"
CREATE INDEX "change_history_object_id_idx" ON "change_history" ("object_id");
-- create "houses" table
CREATE TABLE "houses" ("id" bigint NOT NULL, "object_id" bigint NOT NULL, "object_guid" uuid NOT NULL, "change_id" bigint NOT NULL, "house_num" text NULL, "add_num1" text NULL, "add_num2" text NULL, "house_type" numeric NULL, "add_type1" numeric NULL, "add_type2" numeric NULL, "opertype_id" numeric NOT NULL, "prev_id" bigint NULL, "next_id" bigint NULL, "update_date" date NOT NULL, "start_date" date NOT NULL, "end_date" date NOT NULL, "is_actual" boolean NOT NULL, "is_active" boolean NOT NULL, PRIMARY KEY ("id"));
-- create index "houses_object_id_idx" to table: "houses"
CREATE INDEX "houses_object_id_idx" ON "houses" ("object_id");
-- create "house_types" table
CREATE TABLE "house_types" ("id" bigint NOT NULL, "name" text NOT NULL, "shortname" text NULL, "descr" text NULL, "update_date" date NOT NULL, "start_date" date NOT NULL, "end_date" date NOT NULL, "is_active" boolean NOT NULL, PRIMARY KEY ("id"));
-- create "mun_hierarchy" table
CREATE TABLE "mun_hierarchy" ("id" bigint NOT NULL, "object_id" bigint NOT NULL, "parent_obj_id" bigint NULL, "change_id" bigint NOT NULL, "oktmo" character varying(11) NULL, "prev_id" bigint NULL, "next_id" bigint NULL, "update_date" date NOT NULL, "start_date" date NOT NULL, "end_date" date NOT NULL, "is_active" boolean NOT NULL, "path" text NOT NULL, PRIMARY KEY ("id"));
-- create index "mun_hierarchy_object_id_idx" to table: "mun_hierarchy"
CREATE INDEX "mun_hierarchy_object_id_idx" ON "mun_hierarchy" ("object_id");
-- create index "mun_hierarchy_parent_idx" to table: "mun_hierarchy"
CREATE INDEX "mun_hierarchy_parent_idx" ON "mun_hierarchy" ("parent_obj_id");
-- create "normative_docs" table
CREATE TABLE "normative_docs" ("id" bigint NOT NULL, "name" text NOT NULL, "doc_date" date NOT NULL, "number" text NOT NULL, "type" bigint NOT NULL, "kind" bigint NOT NULL, "update_date" date NOT NULL, "orgname" text NULL, "regnum" text NULL, "reg_date" date NULL, "acc_date" date NULL, "comment" text NULL, PRIMARY KEY ("id"));
-- create "normative_doc_kinds" table
CREATE TABLE "normative_doc_kinds" ("id" numeric NOT NULL, "name" text NOT NULL, PRIMARY KEY ("id"));
-- create "normative_doc_types" table
CREATE TABLE "normative_doc_types" ("id" numeric NOT NULL, "name" text NOT NULL, "start_date" date NOT NULL, "end_date" date NOT NULL, PRIMARY KEY ("id"));
-- create "object_levels" table
CREATE TABLE "object_levels" ("level" bigint NOT NULL, "name" text NOT NULL, "shortname" text NULL, "update_date" date NOT NULL, "start_date" date NOT NULL, "end_date" date NOT NULL, "is_active" boolean NOT NULL, PRIMARY KEY ("level"));
-- create "operation_types" table
CREATE TABLE "operation_types" ("id" numeric NOT NULL, "name" text NOT NULL, "shortname" text NULL, "descr" text NULL, "update_date" date NOT NULL, "start_date" date NOT NULL, "end_date" date NOT NULL, "is_active" boolean NOT NULL, PRIMARY KEY ("id"));
-- create "param" table
CREATE TABLE "param" ("id" bigserial NOT NULL, "source_id" bigint NOT NULL, "source" text NOT NULL DEFAULT '', "object_id" bigint NOT NULL, "change_id" bigint NULL, "change_id_end" bigint NOT NULL, "type_id" bigint NOT NULL, "value" text NOT NULL, "update_date" date NOT NULL, "start_date" date NOT NULL, "end_date" date NOT NULL, PRIMARY KEY ("id"));
-- create index "param_object_type_idx" to table: "param"
CREATE INDEX "param_object_type_idx" ON "param" ("object_id", "type_id");
-- create index "param_source_id_idx" to table: "param"
CREATE UNIQUE INDEX "param_source_id_idx" ON "param" ("source", "source_id");
-- create "param_types" table
CREATE TABLE "param_types" ("id" bigint NOT NULL, "name" text NOT NULL, "code" text NOT NULL, "descr" text NULL, "update_date" date NOT NULL, "start_date" date NOT NULL, "end_date" date NOT NULL, "is_active" boolean NOT NULL, PRIMARY KEY ("id"));
-- create "reestr_objects" table
CREATE TABLE "reestr_objects" ("object_id" bigint NOT NULL, "create_date" date NOT NULL, "change_id" bigint NOT NULL, "level_id" bigint NOT NULL, "update_date" date NOT NULL, "object_guid" uuid NOT NULL, "is_active" boolean NOT NULL, PRIMARY KEY ("object_id"));
-- create index "reestr_objects_guid_idx" to table: "reestr_objects"
CREATE UNIQUE INDEX "reestr_objects_guid_idx" ON "reestr_objects" ("object_guid");
-- create "rooms" table
CREATE TABLE "rooms" ("id" bigint NOT NULL, "object_id" bigint NOT NULL, "object_guid" uuid NOT NULL, "change_id" bigint NOT NULL, "number" text NOT NULL, "room_type" bigint NOT NULL, "opertype_id" bigint NOT NULL, "prev_id" bigint NULL, "next_id" bigint NULL, "update_date" date NOT NULL, "start_date" date NOT NULL, "end_date" date NOT NULL, "is_actual" boolean NOT NULL, "is_active" boolean NOT NULL, PRIMARY KEY ("id"));
-- create index "rooms_object_id_idx" to table: "rooms"
CREATE INDEX "rooms_object_id_idx" ON "rooms" ("object_id");
-- create "room_types" table
CREATE TABLE "room_types" ("id" bigint NOT NULL, "name" text NOT NULL, "shortname" text NULL, "descr" text NULL, "update_date" date NOT NULL, "start_date" date NOT NULL, "end_date" date NOT NULL, "is_active" boolean NOT NULL, PRIMARY KEY ("id"));
-- create "steads" table
CREATE TABLE "steads" ("id" numeric(19) NOT NULL, "object_id" numeric(19) NOT NULL, "object_guid" uuid NOT NULL, "change_id" numeric(19) NOT NULL, "number" text NOT NULL, "opertype_id" character varying(2) NOT NULL, "prev_id" numeric(19) NULL, "next_id" numeric(19) NULL, "update_date" date NOT NULL, "start_date" date NOT NULL, "end_date" date NOT NULL, "is_actual" boolean NOT NULL, "is_active" boolean NOT NULL, PRIMARY KEY ("id"));
-- create index "steads_object_id_idx" to table: "steads"
CREATE INDEX "steads_object_id_idx" ON "steads" ("object_id");
