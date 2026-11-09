-- Seed the items table with a dataset for realistic load tests.
-- Usage (change N by editing 10000 below):
--   psql -U postgres -d items_db -f seed.sql
--   docker exec -i pg psql -U postgres -d items_db < seed.sql

CREATE TABLE IF NOT EXISTS items (id SERIAL PRIMARY KEY, name TEXT);

-- Start clean so ids are exactly 1..N
TRUNCATE items RESTART IDENTITY;

INSERT INTO items (name)
SELECT 'item-' || g
FROM generate_series(1, 10000) AS g;

ANALYZE items;

SELECT count(*) AS rows, min(id) AS min_id, max(id) AS max_id FROM items;