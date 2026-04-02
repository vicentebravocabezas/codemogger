package codemogger

import "context"

type Embedder func(ctx context.Context, texts []string) ([][]float32, error)

type SearchMode string

const (
	SearchModeSemantic SearchMode = "semantic"
	SearchModeKeyword  SearchMode = "keyword"
	SearchModeHybrid   SearchMode = "hybrid"
)

type SearchOptions struct {
	Limit          int
	Threshold      float64
	IncludeSnippet bool
	Mode           SearchMode
}

type IndexPhase string

const (
	IndexPhaseScan    IndexPhase = "scan"
	IndexPhaseHash    IndexPhase = "hash"
	IndexPhaseChunk   IndexPhase = "chunk"
	IndexPhaseEmbed   IndexPhase = "embed"
	IndexPhaseCleanup IndexPhase = "cleanup"
	IndexPhaseFTS     IndexPhase = "fts"
)

type IndexProgress struct {
	Phase   IndexPhase
	Current int
	Total   int
}

type IndexOptions struct {
	Languages  []string
	Verbose    bool
	OnProgress func(IndexProgress)
}

type IndexResult struct {
	Files    int
	Chunks   int
	Embedded int
	Skipped  int
	Removed  int
	Errors   []string
	Duration int64
}

type CodeIndexOptions struct {
	DBPath         string
	Embedder       Embedder
	EmbeddingModel string
}

type SearchResult struct {
	ChunkKey  string
	FilePath  string
	Name      string
	Kind      string
	Signature string
	Snippet   string
	StartLine int
	EndLine   int
	Score     float64
}

type IndexedFile struct {
	FilePath   string
	FileHash   string
	ChunkCount int
	IndexedAt  int64
}

type Codebase struct {
	ID         int64
	RootPath   string
	Name       string
	IndexedAt  int64
	FileCount  int
	ChunkCount int
}

type CodeChunk struct {
	ChunkKey  string
	FilePath  string
	Language  string
	Kind      string
	Name      string
	Signature string
	Snippet   string
	StartLine int
	EndLine   int
	FileHash  string
}
