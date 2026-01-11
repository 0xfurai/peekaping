--bun:split
-- This down migration attempts to restore the unique constraint.
-- Note: This will fail if there are duplicate names in the database.
PRAGMA foreign_keys = off;
CREATE TABLE "new_tags" (
    "id" TEXT PRIMARY KEY,
    "org_id" VARCHAR(255) DEFAULT '',
    "name" VARCHAR(255) NOT NULL UNIQUE,
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