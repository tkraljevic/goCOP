package ledger

// Schema je tablica knjige verzija. Ugrađuje se u InitSchema aplikacije, a
// testovi je koriste izravno.
//
// version_id je UUIDv7 — leksikografski poredak jednak je vremenskom, pa
// "ORDER BY version_id" daje redoslijed nastanka bez oslanjanja na zidne
// satove čvorova, koji na terenu nisu usklađeni.
const Schema = `
CREATE TABLE IF NOT EXISTS record_versions (
	version_id TEXT PRIMARY KEY,
	entity TEXT NOT NULL,
	entity_id TEXT NOT NULL,
	node_id TEXT NOT NULL,
	supersedes TEXT NOT NULL DEFAULT '',
	archived INTEGER NOT NULL DEFAULT 0,
	payload TEXT NOT NULL,
	created_at DATETIME NOT NULL,
	schema_version INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS idx_versions_entity ON record_versions(entity, entity_id, version_id);
CREATE INDEX IF NOT EXISTS idx_versions_node ON record_versions(node_id, version_id);
`
