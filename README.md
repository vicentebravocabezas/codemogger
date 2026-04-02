# codemogger

`codemogger` is a Go SDK for indexing codebases for AI agents and developer tools.
It parses source files with tree-sitter, chunks them into semantic units, stores
them in SQLite, and supports both keyword and semantic search.

This port is SDK-only. The TypeScript CLI and MCP server are intentionally not
carried over.

## Install

```bash
go get github.com/glommer/codemogger
```

## Quick start

```go
package main

import (
	"context"
	"fmt"

	"github.com/glommer/codemogger"
)

func embedder(_ context.Context, texts []string) ([][]float32, error) {
	vectors := make([][]float32, len(texts))
	for i := range texts {
		vectors[i] = make([]float32, 384)
	}
	return vectors, nil
}

func main() {
	ctx := context.Background()

	dbPath, err := codemogger.ProjectDBPath("./my-project")
	if err != nil {
		panic(err)
	}

	index, err := codemogger.New(codemogger.CodeIndexOptions{
		DBPath:         dbPath,
		Embedder:       embedder,
		EmbeddingModel: "my-embedder",
	})
	if err != nil {
		panic(err)
	}
	defer index.Close()

	if _, err := index.Index(ctx, "./my-project", nil); err != nil {
		panic(err)
	}

	results, err := index.Search(ctx, "authentication middleware", &codemogger.SearchOptions{
		Mode:  codemogger.SearchModeHybrid,
		Limit: 5,
	})
	if err != nil {
		panic(err)
	}

	for _, result := range results {
		fmt.Println(result.FilePath, result.Name, result.Score)
	}
}
```

## API

```go
type Embedder func(ctx context.Context, texts []string) ([][]float32, error)

func ProjectDBPath(dir string) (string, error)
func New(opts CodeIndexOptions) (*CodeIndex, error)

type CodeIndex struct {
	Index(ctx context.Context, dir string, opts *IndexOptions) (IndexResult, error)
	Search(ctx context.Context, query string, opts *SearchOptions) ([]SearchResult, error)
	ListFiles(ctx context.Context) ([]IndexedFile, error)
	ListCodebases(ctx context.Context) ([]Codebase, error)
	Close() error
}
```

## Search modes

- `semantic`: embeds the query and scores chunks by cosine similarity.
- `keyword`: runs SQLite FTS5 over chunk names and signatures.
- `hybrid`: merges semantic and keyword rankings with reciprocal rank fusion.

## How indexing works

1. Walk the target directory and filter supported source files.
2. Hash file contents and skip unchanged files.
3. Parse changed files with tree-sitter and extract top-level definitions.
4. Call your embedder for stale chunks.
5. Store chunks, metadata, and embeddings in a single SQLite database.
6. Rebuild the FTS index for keyword search.

## Supported languages

Current Go SDK support:

- Rust
- C
- C++
- Go
- Python
- Java
- JavaScript
- TypeScript
- TSX
- PHP
- Ruby

The original TypeScript project also listed Zig, Scala, and C#. Those are not
included in this Go port yet because equivalent Go tree-sitter bindings were not
available in the same shape as the rest of the grammars.

## Notes

- Hidden directories and common build/vendor directories are skipped.
- `.gitignore` entries are respected in a simplified directory-name form.
- Files larger than 1 MB are skipped.
- Embeddings are supplied by the caller; `codemogger` does not ship a model.
- The database lives under `.codemogger/index.db` when you use `ProjectDBPath`.

## License

MIT
