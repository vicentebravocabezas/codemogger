package codemogger

import (
	"context"
	"hash/fnv"
	"os"
	"path/filepath"
	"testing"
)

func hashedEmbedder(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, text := range texts {
		vec := make([]float32, 64)
		for _, token := range tokenRE.FindAllString(toLower(text), -1) {
			h := fnv.New32a()
			_, _ = h.Write([]byte(token))
			vec[h.Sum32()%uint32(len(vec))] += 1
		}
		out[i] = vec
	}
	return out, nil
}

func TestCodeIndex_IndexAndSearch(t *testing.T) {
	ctx := context.Background()
	rootDir := t.TempDir()

	source := `package auth

// LoginUser validates authentication credentials and creates a session.
func LoginUser(username string, password string) bool {
	return username != "" && password != ""
}

func logoutUser() {}
`
	if err := os.WriteFile(filepath.Join(rootDir, "auth.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	dbPath, err := ProjectDBPath(rootDir)
	if err != nil {
		t.Fatal(err)
	}

	index, err := New(CodeIndexOptions{
		DBPath:   dbPath,
		Embedder: hashedEmbedder,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = index.Close()
	}()

	result, err := index.Index(ctx, rootDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Files != 1 {
		t.Fatalf("expected 1 processed file, got %d", result.Files)
	}
	if result.Chunks == 0 {
		t.Fatalf("expected chunks to be indexed")
	}
	if result.Embedded == 0 {
		t.Fatalf("expected embeddings to be stored")
	}

	keywordResults, err := index.Search(ctx, "LoginUser", &SearchOptions{
		Mode:           SearchModeKeyword,
		Limit:          3,
		IncludeSnippet: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(keywordResults) == 0 {
		t.Fatalf("expected keyword results")
	}
	if keywordResults[0].Name != "LoginUser" {
		t.Fatalf("expected LoginUser, got %#v", keywordResults[0])
	}

	semanticResults, err := index.Search(ctx, "authentication login", &SearchOptions{
		Mode:  SearchModeSemantic,
		Limit: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(semanticResults) == 0 {
		t.Fatalf("expected semantic results")
	}
	if semanticResults[0].Name != "LoginUser" {
		t.Fatalf("expected LoginUser, got %#v", semanticResults[0])
	}
}
