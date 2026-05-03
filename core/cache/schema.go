package cache

// schemaStatements lists every CREATE-style DDL the cache file needs.
// Every statement is idempotent — pure CREATE IF NOT EXISTS — so they
// re-run on every Open without harm. Same pattern noted in the
// project's CLAUDE.md "Container Deploys" section: migrations that
// run at start-up MUST tolerate being run twice.
var schemaStatements = []string{
	`CREATE TABLE IF NOT EXISTS objects (
        kind          TEXT NOT NULL,
        owner         TEXT NOT NULL,
        repo          TEXT NOT NULL,
        id            TEXT NOT NULL,
        etag          TEXT,
        last_modified TEXT,
        fetched_at    INTEGER NOT NULL,
        ttl_seconds   INTEGER NOT NULL,
        payload       BLOB NOT NULL,
        PRIMARY KEY (kind, owner, repo, id)
    )`,
	`CREATE TABLE IF NOT EXISTS list_index (
        kind          TEXT NOT NULL,
        owner         TEXT NOT NULL,
        repo          TEXT NOT NULL,
        query_hash    TEXT NOT NULL,
        fetched_at    INTEGER NOT NULL,
        ttl_seconds   INTEGER NOT NULL,
        next_cursor   TEXT,
        payload       BLOB NOT NULL,
        PRIMARY KEY (kind, owner, repo, query_hash)
    )`,
	`CREATE INDEX IF NOT EXISTS objects_fetched_at ON objects(fetched_at)`,
	`CREATE INDEX IF NOT EXISTS list_index_fetched_at ON list_index(fetched_at)`,
}

// applySchema runs every DDL on the open DB. Returns the first error.
func (c *Cache) applySchema() error {
	for _, stmt := range schemaStatements {
		if _, err := c.db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}
