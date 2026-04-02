package codemogger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type CodeIndex struct {
	store          *store
	dbPath         string
	embedder       Embedder
	embeddingModel string
	searchVerified bool
}

func ProjectDBPath(dir string) (string, error) {
	rootDir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	dbDir := filepath.Join(rootDir, ".codemogger")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dbDir, "index.db"), nil
}

func New(opts CodeIndexOptions) (*CodeIndex, error) {
	if opts.DBPath == "" {
		return nil, errors.New("db path is required")
	}
	if opts.Embedder == nil {
		return nil, errors.New("embedder is required")
	}

	st, err := openStore(opts.DBPath)
	if err != nil {
		return nil, err
	}

	return &CodeIndex{
		store:          st,
		dbPath:         opts.DBPath,
		embedder:       opts.Embedder,
		embeddingModel: "bge-m3",
	}, nil
}

func (c *CodeIndex) Index(ctx context.Context, dir string, opts *IndexOptions) (IndexResult, error) {
	start := time.Now()
	rootDir, err := filepath.Abs(dir)
	if err != nil {
		return IndexResult{}, err
	}

	codebaseID, err := c.store.getOrCreateCodebase(ctx, rootDir)
	if err != nil {
		return IndexResult{}, err
	}

	progress := progressReporter(opts)

	t0 := time.Now()
	progress(IndexProgress{Phase: IndexPhaseScan})
	files, scanErrors := scanDirectory(ctx, rootDir, optsLanguages(opts))
	scanTime := time.Since(t0).Milliseconds()

	result := IndexResult{Errors: scanErrors}
	activeFiles := make(map[string]struct{}, len(files))
	filesToProcess := make([]scannedFile, 0, len(files))

	for i, file := range files {
		activeFiles[file.AbsPath] = struct{}{}
		storedHash, err := c.store.getFileHash(ctx, codebaseID, file.AbsPath)
		if err != nil {
			return IndexResult{}, err
		}
		if storedHash == file.Hash {
			result.Skipped++
		} else {
			filesToProcess = append(filesToProcess, file)
		}
		progress(IndexProgress{Phase: IndexPhaseHash, Current: i + 1, Total: len(files)})
	}

	const fileBatch = 200
	t1 := time.Now()
	for batchStart := 0; batchStart < len(filesToProcess); batchStart += fileBatch {
		end := min(batchStart+fileBatch, len(filesToProcess))
		batch := filesToProcess[batchStart:end]
		batchChunks := make([]struct {
			FilePath string
			FileHash string
			Chunks   []CodeChunk
		}, 0, len(batch))

		for i, file := range batch {
			cfg := detectLanguage(file.AbsPath)
			if cfg == nil {
				continue
			}
			chunks, err := chunkFile(file.AbsPath, file.Content, file.Hash, cfg)
			if err != nil {
				result.Errors = append(result.Errors, file.AbsPath+": "+err.Error())
			} else {
				batchChunks = append(batchChunks, struct {
					FilePath string
					FileHash string
					Chunks   []CodeChunk
				}{
					FilePath: file.AbsPath,
					FileHash: file.Hash,
					Chunks:   chunks,
				})
				result.Files++
				result.Chunks += len(chunks)
			}
			progress(IndexProgress{Phase: IndexPhaseChunk, Current: batchStart + i + 1, Total: len(filesToProcess)})
			if i == 1 {
				jsonBytes, _ := json.MarshalIndent(batchChunks, "", "  ")
				fmt.Printf("%s\n", jsonBytes)
			}
		}

		if len(batchChunks) > 0 {
			if err := c.store.batchUpsertAllFileChunks(ctx, codebaseID, batchChunks); err != nil {
				return IndexResult{}, err
			}
		}
	}

	embedTotal, err := c.store.countStaleEmbeddings(ctx, codebaseID, c.embeddingModel)
	if err != nil {
		return IndexResult{}, err
	}

	const embedBatch = 64
	for {
		stale, err := c.store.getStaleEmbeddings(ctx, codebaseID, c.embeddingModel, 1000)
		if err != nil {
			return IndexResult{}, err
		}
		if len(stale) == 0 {
			break
		}

		for i := 0; i < len(stale); i += embedBatch {
			end := min(i+embedBatch, len(stale))
			slice := stale[i:end]
			texts := make([]string, len(slice))
			for j, item := range slice {
				texts[j] = buildEmbedText(item.FilePath, item.Kind, item.Name, item.Signature, item.Snippet)
			}
			vectors, err := c.embedder(ctx, texts)
			if err != nil {
				return IndexResult{}, err
			}
			if len(vectors) != len(slice) {
				return IndexResult{}, fmt.Errorf("embedder returned %d vectors for %d texts", len(vectors), len(slice))
			}

			updates := make([]embeddingUpdate, len(slice))
			for j, item := range slice {
				updates[j] = embeddingUpdate{
					ChunkKey:  item.ChunkKey,
					Embedding: vectors[j],
					ModelName: c.embeddingModel,
				}
			}
			if err := c.store.batchUpsertEmbeddings(ctx, updates); err != nil {
				return IndexResult{}, err
			}
			result.Embedded += len(updates)
			progress(IndexProgress{Phase: IndexPhaseEmbed, Current: result.Embedded, Total: embedTotal})
		}

		if len(stale) < 1000 {
			break
		}
	}
	chunkAndEmbedTime := time.Since(t1).Milliseconds()

	progress(IndexProgress{Phase: IndexPhaseCleanup})
	removed, err := c.store.removeStaleFiles(ctx, codebaseID, activeFiles)
	if err != nil {
		return IndexResult{}, err
	}
	result.Removed = removed

	progress(IndexProgress{Phase: IndexPhaseFTS})
	t2 := time.Now()
	if err := c.store.rebuildFTS(ctx); err != nil {
		return IndexResult{}, err
	}
	ftsTime := time.Since(t2).Milliseconds()

	if err := c.store.touchCodebase(ctx, codebaseID); err != nil {
		return IndexResult{}, err
	}

	result.Duration = time.Since(start).Milliseconds()
	if opts != nil && opts.Verbose {
		fmt.Printf("  scan: %dms, chunk+embed: %dms (%d chunks), fts: %dms\n", scanTime, chunkAndEmbedTime, result.Embedded, ftsTime)
	}
	return result, nil
}

func (c *CodeIndex) Search(ctx context.Context, query string, opts *SearchOptions) ([]SearchResult, error) {
	limit := 5
	threshold := 0.0
	includeSnippet := false
	mode := SearchModeSemantic
	if opts != nil {
		if opts.Limit > 0 {
			limit = opts.Limit
		}
		threshold = opts.Threshold
		includeSnippet = opts.IncludeSnippet
		if opts.Mode != "" {
			mode = opts.Mode
		}
	}

	if err := c.verifySearchable(ctx); err != nil {
		return nil, err
	}

	switch mode {
	case SearchModeSemantic:
		vectors, err := c.embedder(ctx, []string{query})
		if err != nil {
			return nil, err
		}
		if len(vectors) != 1 {
			return nil, fmt.Errorf("embedder returned %d vectors for 1 query", len(vectors))
		}
		results, err := c.store.vectorSearch(ctx, vectors[0], limit, includeSnippet)
		if err != nil {
			return nil, err
		}
		return filterByThreshold(results, threshold), nil
	case SearchModeKeyword:
		processed := preprocessQuery(query, "keywords")
		if strings.TrimSpace(processed) == "" {
			return nil, nil
		}
		results, err := c.store.ftsSearch(ctx, processed, limit, includeSnippet)
		if err != nil {
			return nil, err
		}
		return filterByThreshold(results, threshold), nil
	case SearchModeHybrid:
		processed := preprocessQuery(query, "keywords")
		var ftsResults []SearchResult
		if strings.TrimSpace(processed) != "" {
			var err error
			ftsResults, err = c.store.ftsSearch(ctx, processed, limit, includeSnippet)
			if err != nil {
				return nil, err
			}
		}
		vectors, err := c.embedder(ctx, []string{query})
		if err != nil {
			return nil, err
		}
		if len(vectors) != 1 {
			return nil, fmt.Errorf("embedder returned %d vectors for 1 query", len(vectors))
		}
		vecResults, err := c.store.vectorSearch(ctx, vectors[0], limit, includeSnippet)
		if err != nil {
			return nil, err
		}
		return filterByThreshold(rrfMerge(ftsResults, vecResults, limit, 60, 0.4, 0.6), threshold), nil
	default:
		return nil, fmt.Errorf("unsupported search mode %q", mode)
	}
}

func (c *CodeIndex) verifySearchable(ctx context.Context) error {
	if c.searchVerified {
		return nil
	}
	c.searchVerified = true

	info, err := os.Stat(c.dbPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if info.Size() <= 1_000_000 {
		return nil
	}

	codebases, err := c.store.listCodebases(ctx)
	if err != nil {
		return err
	}
	if len(codebases) == 0 {
		return nil
	}

	totalChunks := 0
	for _, codebase := range codebases {
		totalChunks += codebase.ChunkCount
	}
	if totalChunks > 0 {
		return nil
	}

	return fmt.Errorf("database file is %dMB but contains no indexed chunks", info.Size()/1_000_000)
}

func (c *CodeIndex) ListFiles(ctx context.Context) ([]IndexedFile, error) {
	return c.store.listFiles(ctx)
}

func (c *CodeIndex) ListCodebases(ctx context.Context) ([]Codebase, error) {
	return c.store.listCodebases(ctx)
}

func (c *CodeIndex) Close() error {
	if c.store == nil {
		return nil
	}
	err := c.store.close()
	c.store = nil
	return err
}

func buildEmbedText(filePath, kind, name, signature, snippet string) string {
	text := filePath
	if kind != "" && name != "" {
		text += ": " + kind + " " + name
	} else if name != "" {
		text += ": " + name
	}
	if signature != "" {
		text += "\n" + signature
	}
	if snippet != "" {
		preview := snippet
		if len(preview) > 500 {
			preview = preview[:500]
		}
		text += "\n" + preview
	}
	return text
}

func progressReporter(opts *IndexOptions) func(IndexProgress) {
	if opts == nil || opts.OnProgress == nil {
		return func(IndexProgress) {}
	}
	lastPct := -1
	lastPhase := IndexPhase("")
	return func(progress IndexProgress) {
		if progress.Phase != lastPhase {
			lastPhase = progress.Phase
			lastPct = -1
		}
		if progress.Total > 0 {
			pct := progress.Current * 100 / progress.Total
			if pct == lastPct {
				return
			}
			lastPct = pct
		}
		opts.OnProgress(progress)
	}
}

func optsLanguages(opts *IndexOptions) []string {
	if opts == nil {
		return nil
	}
	return opts.Languages
}

func filterByThreshold(results []SearchResult, threshold float64) []SearchResult {
	if threshold <= 0 {
		return results
	}
	out := make([]SearchResult, 0, len(results))
	for _, result := range results {
		if result.Score >= threshold {
			out = append(out, result)
		}
	}
	return out
}
