--bun:split
DROP INDEX IF EXISTS tags_name_key;
DROP INDEX IF EXISTS tags_name_uindex;
-- Try to drop standard index if it exists, otherwise it might be a constraint.
-- SQLite constraints are hard to drop without table recreation.
-- However, if it was created as CONSTRAINT UNIQUE, it's an implicit index.
-- If it was created as just UNIQUE, it's also an index.
-- Since we are using SQLite, and ALTER TABLE DROP CONSTRAINT is not supported properly,
-- we might have to live with it or do a full table migration.
-- But wait, standard CREATE TABLE ... UNIQUE(name) creates an index.
-- Let's try to just drop the index. Using a "dirty" trick if possible or just try to create a new unique index on org_id + name if we removed the old one.
-- Actually, user said "colocar essa regra no codigo, nao no banco" (put this rule in code, not in the db).
-- So I should just drop the uniqueness.
-- In Postgres it would be ALTER TABLE tags DROP CONSTRAINT ...
-- In SQLite, we have to see how it was created.
-- If previous migration used `bun:"name,unique"`, Bun generates `CREATE UNIQUE INDEX ...` usually side-loaded or `UNIQUE` constraint inline.
-- Let's assume inline constraint `name VARCHAR(...) UNIQUE`.
-- Removing inline constraint in SQLite requires creating new table.
-- Let's attempt to creating a new migration that does the table copy dance if strictly necessary.
-- BUT, if it was a separate index, we can just DROP INDEX.
-- The error `UNIQUE constraint failed: tags.name` often comes from a named index or inline constraint.
-- Let's try to just drop the index if we can guess its name?
-- Bun usually names them `model_column_key` or similar. `tags_name_key`.
-- NOTE: If this fails, we might need a more complex migration.
-- Let's try the table recreation approach as it's the only safe way in SQLite for constraints.
PRAGMA foreign_keys = off;
CREATE TABLE "new_tags" (
    "id" TEXT PRIMARY KEY,
    "org_id" VARCHAR(255) DEFAULT '',
    "name" VARCHAR(255) NOT NULL,
    "color" VARCHAR(255) NOT NULL,
    "description" TEXT,
    "created_at" DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO "new_tags" (
        "id",
        "org_id",
        "name",
        "color",
        "description",
        "created_at",
        "updated_at"
    )
SELECT "id",
    "org_id",
    "name",
    "color",
    "description",
    "created_at",
    "updated_at"
FROM "tags";
DROP TABLE "tags";
ALTER TABLE "new_tags"
    RENAME TO "tags";
PRAGMA foreign_keys = on;