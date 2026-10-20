CREATE INDEX "addr_obj_latest_idx" ON "addr_obj" ("object_id", "is_active" DESC, "is_actual" DESC, "end_date" DESC, "id" DESC);
CREATE INDEX "houses_latest_idx" ON "houses" ("object_id", "is_active" DESC, "is_actual" DESC, "end_date" DESC, "id" DESC);
CREATE INDEX "apartments_latest_idx" ON "apartments" ("object_id", "is_active" DESC, "is_actual" DESC, "end_date" DESC, "id" DESC);
CREATE INDEX "rooms_latest_idx" ON "rooms" ("object_id", "is_active" DESC, "is_actual" DESC, "end_date" DESC, "id" DESC);
CREATE INDEX "steads_latest_idx" ON "steads" ("object_id", "is_active" DESC, "is_actual" DESC, "end_date" DESC, "id" DESC);
CREATE INDEX "carplaces_latest_idx" ON "carplaces" ("object_id", "is_active" DESC, "is_actual" DESC, "end_date" DESC, "id" DESC);
CREATE INDEX "adm_hierarchy_region_object_idx" ON "adm_hierarchy" ("region_code", "object_id");
CREATE INDEX "adm_hierarchy_latest_idx" ON "adm_hierarchy" ("object_id", "is_active" DESC, "end_date" DESC, "id" DESC);
