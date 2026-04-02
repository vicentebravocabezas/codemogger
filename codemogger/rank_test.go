package codemogger

import "testing"

func testResult(name string, score float64) SearchResult {
	return SearchResult{
		ChunkKey:  name,
		FilePath:  "/src/" + name + ".go",
		Name:      name,
		Kind:      "function",
		Signature: "func " + name + "()",
		Snippet:   "func " + name + "() {}",
		StartLine: 1,
		EndLine:   5,
		Score:     score,
	}
}

func TestRRFMerge(t *testing.T) {
	t.Run("boosts results that appear in both lists", func(t *testing.T) {
		fts := []SearchResult{testResult("a", 1), testResult("b", 1), testResult("c", 1)}
		vec := []SearchResult{testResult("b", 1), testResult("a", 1), testResult("d", 1)}

		merged := rrfMerge(fts, vec, 5, 60, 0.4, 0.6)
		if len(merged) != 4 {
			t.Fatalf("got %d results", len(merged))
		}
		topTwo := map[string]struct{}{
			merged[0].Name: {},
			merged[1].Name: {},
		}
		if _, ok := topTwo["a"]; !ok {
			t.Fatalf("top results missing a: %#v", merged[:2])
		}
		if _, ok := topTwo["b"]; !ok {
			t.Fatalf("top results missing b: %#v", merged[:2])
		}
		if merged[0].Score <= merged[2].Score {
			t.Fatalf("expected fused score ordering, got %#v", merged)
		}
	})

	t.Run("respects limit", func(t *testing.T) {
		fts := []SearchResult{testResult("a", 1), testResult("b", 1), testResult("c", 1)}
		vec := []SearchResult{testResult("d", 1), testResult("e", 1), testResult("f", 1)}
		if got := len(rrfMerge(fts, vec, 2, 60, 0.4, 0.6)); got != 2 {
			t.Fatalf("got %d results", got)
		}
	})

	t.Run("handles empty lists", func(t *testing.T) {
		if got := rrfMerge(nil, []SearchResult{testResult("a", 1)}, 5, 60, 0.4, 0.6); len(got) != 1 {
			t.Fatalf("got %d results", len(got))
		}
		if got := rrfMerge([]SearchResult{testResult("a", 1)}, nil, 5, 60, 0.4, 0.6); len(got) != 1 {
			t.Fatalf("got %d results", len(got))
		}
		if got := rrfMerge(nil, nil, 5, 60, 0.4, 0.6); len(got) != 0 {
			t.Fatalf("got %d results", len(got))
		}
	})
}
