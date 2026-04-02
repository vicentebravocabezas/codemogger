package codemogger

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	_ "github.com/tursodatabase/go-libsql"
)

type staleEmbedding struct {
	ChunkKey  string
	Name      string
	Signature string
	FilePath  string
	Kind      string
	Snippet   string
}

type embeddingUpdate struct {
	ChunkKey  string
	Embedding []float32
	ModelName string
}

type store struct {
	db *sql.DB
}

func openStore(dbPath string) (*store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("libsql", fmt.Sprintf("file:%s", dbPath))
	if err != nil {
		return nil, err
	}
	s := &store{db: db}
	if err := s.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *store) init() error {
	// statements := []string{
	// 	`PRAGMA journal_mode = WAL`,
	// 	`PRAGMA foreign_keys = ON`,
	// 	`PRAGMA busy_timeout = 5000`,
	// }
	// for _, stmt := range statements {
	// 	if _, err := s.db.Exec(stmt); err != nil {
	// 		return err
	// 	}
	// }
	for _, stmt := range baseSchema {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *store) getOrCreateCodebase(ctx context.Context, rootPath string) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM codebases WHERE root_path = ?`, rootPath).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}

	now := time.Now().UnixMilli()
	name := baseName(rootPath)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO codebases (root_path, name, indexed_at) VALUES (?, ?, ?)`, rootPath, name, now); err != nil {
		return 0, err
	}
	err = s.db.QueryRowContext(ctx, `SELECT id FROM codebases WHERE root_path = ?`, rootPath).Scan(&id)
	return id, err
}

func (s *store) listCodebases(ctx context.Context) ([]Codebase, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.root_path, c.name, c.indexed_at,
		       COUNT(DISTINCT f.file_path) AS file_count,
		       COALESCE(SUM(f.chunk_count), 0) AS chunk_count
		FROM codebases c
		LEFT JOIN indexed_files f ON f.codebase_id = c.id
		GROUP BY c.id
		ORDER BY c.root_path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Codebase
	for rows.Next() {
		var item Codebase
		if err := rows.Scan(&item.ID, &item.RootPath, &item.Name, &item.IndexedAt, &item.FileCount, &item.ChunkCount); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *store) touchCodebase(ctx context.Context, codebaseID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE codebases SET indexed_at = ? WHERE id = ?`, time.Now().UnixMilli(), codebaseID)
	return err
}

func (s *store) getFileHash(ctx context.Context, codebaseID int64, filePath string) (string, error) {
	var hash string
	err := s.db.QueryRowContext(ctx, `SELECT file_hash FROM indexed_files WHERE codebase_id = ? AND file_path = ?`, codebaseID, filePath).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return hash, err
}

func (s *store) batchUpsertAllFileChunks(ctx context.Context, codebaseID int64, fileChunks []struct {
	FilePath string
	FileHash string
	Chunks   []CodeChunk
}) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	now := time.Now().UnixMilli()
	deleteStmt, err := tx.PrepareContext(ctx, `DELETE FROM chunks WHERE codebase_id = ? AND file_path = ?`)
	if err != nil {
		return err
	}
	defer deleteStmt.Close()

	insertStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO chunks (
			codebase_id, file_path, chunk_key, language, kind, name, signature, snippet,
			start_line, end_line, file_hash, indexed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(chunk_key) DO UPDATE SET
			codebase_id = excluded.codebase_id,
			file_path = excluded.file_path,
			language = excluded.language,
			kind = excluded.kind,
			name = excluded.name,
			signature = excluded.signature,
			snippet = excluded.snippet,
			start_line = excluded.start_line,
			end_line = excluded.end_line,
			file_hash = excluded.file_hash,
			indexed_at = excluded.indexed_at,
			embedding = NULL,
			embedding_model = ''`)
	if err != nil {
		return err
	}
	defer insertStmt.Close()

	fileStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO indexed_files (codebase_id, file_path, file_hash, chunk_count, indexed_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(codebase_id, file_path) DO UPDATE SET
			file_hash = excluded.file_hash,
			chunk_count = excluded.chunk_count,
			indexed_at = excluded.indexed_at`)
	if err != nil {
		return err
	}
	defer fileStmt.Close()

	for _, item := range fileChunks {
		if _, err = deleteStmt.ExecContext(ctx, codebaseID, item.FilePath); err != nil {
			return err
		}
		for _, chunk := range item.Chunks {
			if _, err = insertStmt.ExecContext(
				ctx,
				codebaseID,
				chunk.FilePath,
				chunk.ChunkKey,
				chunk.Language,
				chunk.Kind,
				chunk.Name,
				chunk.Signature,
				chunk.Snippet,
				chunk.StartLine,
				chunk.EndLine,
				chunk.FileHash,
				now,
			); err != nil {
				return err
			}
		}
		if _, err = fileStmt.ExecContext(ctx, codebaseID, item.FilePath, item.FileHash, len(item.Chunks), now); err != nil {
			return err
		}
	}

	err = tx.Commit()
	return err
}

func (s *store) removeStaleFiles(ctx context.Context, codebaseID int64, activeFiles map[string]struct{}) (int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT file_path FROM indexed_files WHERE codebase_id = ?`, codebaseID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var all []string
	for rows.Next() {
		var filePath string
		if err := rows.Scan(&filePath); err != nil {
			return 0, err
		}
		all = append(all, filePath)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	removed := 0
	for _, filePath := range all {
		if _, ok := activeFiles[filePath]; ok {
			continue
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM chunks WHERE codebase_id = ? AND file_path = ?`, codebaseID, filePath); err != nil {
			return 0, err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM indexed_files WHERE codebase_id = ? AND file_path = ?`, codebaseID, filePath); err != nil {
			return 0, err
		}
		removed++
	}

	err = tx.Commit()
	return removed, err
}

func encodeEmbedding(vec []float32) []byte {
	buf := make([]byte, len(vec)*4)
	for i, v := range vec {
		binary.LittleEndian.PutUint32(buf[i*4:], mathFloat32bits(v))
	}
	return buf
}

func decodeEmbedding(blob []byte) []float32 {
	if len(blob)%4 != 0 {
		return nil
	}
	out := make([]float32, len(blob)/4)
	for i := range out {
		out[i] = mathFloat32frombits(binary.LittleEndian.Uint32(blob[i*4:]))
	}
	return out
}

func (s *store) batchUpsertEmbeddings(ctx context.Context, items []embeddingUpdate) error {
	if len(items) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	stmt, err := tx.PrepareContext(ctx, `UPDATE chunks SET embedding = vector8(?), embedding_model = ? WHERE chunk_key = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, item := range items {
		if _, err = stmt.ExecContext(ctx, encodeEmbedding(item.Embedding), item.ModelName, item.ChunkKey); err != nil {
			return err
		}
	}

	err = tx.Commit()
	return err
}

func (s *store) countStaleEmbeddings(ctx context.Context, codebaseID int64, modelName string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM chunks WHERE codebase_id = ? AND (embedding IS NULL OR embedding_model != ?)`, codebaseID, modelName).Scan(&count)
	return count, err
}

func (s *store) getStaleEmbeddings(ctx context.Context, codebaseID int64, modelName string, limit int) ([]staleEmbedding, error) {
	query := `SELECT chunk_key, name, signature, file_path, kind, snippet
		FROM chunks
		WHERE codebase_id = ? AND (embedding IS NULL OR embedding_model != ?)`
	args := []any{codebaseID, modelName}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []staleEmbedding
	for rows.Next() {
		var item staleEmbedding
		if err := rows.Scan(&item.ChunkKey, &item.Name, &item.Signature, &item.FilePath, &item.Kind, &item.Snippet); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *store) rebuildFTS(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	for _, stmt := range []string{dropFTSSQL, createFTSSQL, populateFTSSQL} {
		if _, err = tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}

	err = tx.Commit()
	return err
}

func (s *store) vectorSearch(ctx context.Context, queryEmbedding []float32, limit int, includeSnippet bool) ([]SearchResult, error) {
	query := `SELECT chunk_key, file_path, name, kind, signature, start_line, end_line,
                vector_distance_cos(embedding, vector8(?)) AS distance
         FROM chunks
         WHERE embedding IS NOT NULL
         ORDER BY distance ASC
         LIMIT ?`
	if includeSnippet {
		query = `SELECT chunk_key, file_path, name, kind, signature, snippet, start_line, end_line,
                vector_distance_cos(embedding, vector8(?)) AS distance
         FROM chunks
         WHERE embedding IS NOT NULL
         ORDER BY distance ASC
         LIMIT ?`
	}

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make([]SearchResult, 0, limit)
	for rows.Next() {
		var item SearchResult
		var blob []byte
		if includeSnippet {
			if err := rows.Scan(&item.ChunkKey, &item.FilePath, &item.Name, &item.Kind, &item.Signature, &item.Snippet, &item.StartLine, &item.EndLine, &blob); err != nil {
				return nil, err
			}
		} else {
			if err := rows.Scan(&item.ChunkKey, &item.FilePath, &item.Name, &item.Kind, &item.Signature, &item.StartLine, &item.EndLine, &blob); err != nil {
				return nil, err
			}
		}
		item.Score = cosineSimilarity(queryEmbedding, decodeEmbedding(blob))
		results = append(results, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func (s *store) ftsSearch(ctx context.Context, query string, limit int, includeSnippet bool) ([]SearchResult, error) {
	selectCols := `c.chunk_key, c.file_path, c.name, c.kind, c.signature, c.start_line, c.end_line`
	if includeSnippet {
		selectCols = `c.chunk_key, c.file_path, c.name, c.kind, c.signature, c.snippet, c.start_line, c.end_line`
	}

	sqlQuery := `SELECT ` + selectCols + `,
		-bm25(chunks_fts, 5.0, 3.0) AS score
		FROM chunks_fts
		JOIN chunks c ON c.id = chunks_fts.rowid
		WHERE chunks_fts MATCH ?
		ORDER BY bm25(chunks_fts, 5.0, 3.0)
		LIMIT ?`

	rows, err := s.db.QueryContext(ctx, sqlQuery, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var item SearchResult
		if includeSnippet {
			if err := rows.Scan(&item.ChunkKey, &item.FilePath, &item.Name, &item.Kind, &item.Signature, &item.Snippet, &item.StartLine, &item.EndLine, &item.Score); err != nil {
				return nil, err
			}
		} else {
			if err := rows.Scan(&item.ChunkKey, &item.FilePath, &item.Name, &item.Kind, &item.Signature, &item.StartLine, &item.EndLine, &item.Score); err != nil {
				return nil, err
			}
		}
		results = append(results, item)
	}
	return results, rows.Err()
}

func (s *store) listFiles(ctx context.Context) ([]IndexedFile, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT file_path, file_hash, chunk_count, indexed_at FROM indexed_files ORDER BY file_path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []IndexedFile
	for rows.Next() {
		var item IndexedFile
		if err := rows.Scan(&item.FilePath, &item.FileHash, &item.ChunkCount, &item.IndexedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *store) close() error {
	return s.db.Close()
}
