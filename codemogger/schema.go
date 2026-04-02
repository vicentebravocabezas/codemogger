package codemogger

var baseSchema = []string{
	`CREATE TABLE IF NOT EXISTS codebases (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		root_path TEXT NOT NULL UNIQUE,
		name TEXT NOT NULL DEFAULT '',
		indexed_at INTEGER NOT NULL DEFAULT 0
	)`,
	`CREATE TABLE IF NOT EXISTS chunks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		codebase_id INTEGER NOT NULL REFERENCES codebases(id),
		file_path TEXT NOT NULL,
		chunk_key TEXT NOT NULL UNIQUE,
		language TEXT NOT NULL,
		kind TEXT NOT NULL,
		name TEXT NOT NULL DEFAULT '',
		signature TEXT NOT NULL DEFAULT '',
		snippet TEXT NOT NULL,
		start_line INTEGER NOT NULL,
		end_line INTEGER NOT NULL,
		file_hash TEXT NOT NULL,
		indexed_at INTEGER NOT NULL,
		embedding vector8(1024),
		embedding_model TEXT NOT NULL DEFAULT ''
	)`,
	`CREATE INDEX IF NOT EXISTS idx_chunks_codebase_file ON chunks(codebase_id, file_path)`,
	`CREATE TABLE IF NOT EXISTS indexed_files (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		codebase_id INTEGER NOT NULL REFERENCES codebases(id),
		file_path TEXT NOT NULL,
		file_hash TEXT NOT NULL,
		chunk_count INTEGER NOT NULL DEFAULT 0,
		indexed_at INTEGER NOT NULL,
		UNIQUE(codebase_id, file_path)
	)`,
}

const dropFTSSQL = `DROP TABLE IF EXISTS chunks_fts`

const createFTSSQL = `CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(
	name,
	signature,
	tokenize='unicode61'
)`

const populateFTSSQL = `INSERT INTO chunks_fts(rowid, name, signature)
SELECT id, name, signature FROM chunks`
