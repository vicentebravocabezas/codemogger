package codemogger

import (
	"strings"
	"testing"
)

func TestExtractKeywords(t *testing.T) {
	t.Run("removes stopwords and short tokens", func(t *testing.T) {
		if got := ExtractKeywords("I want to extract text from a PDF file"); got != "extract text pdf" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("preserves hyphenated terms", func(t *testing.T) {
		if got := ExtractKeywords("set up a react-setup project with typescript"); got != "set react-setup project typescript" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("deduplicates tokens", func(t *testing.T) {
		if got := ExtractKeywords("review the code review for code quality"); got != "review quality" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("caps at twelve terms", func(t *testing.T) {
		longPrompt := "alpha bravo charlie delta echo foxtrot golf hotel india juliet kilo lima mike november oscar papa"
		if count := len(strings.Fields(ExtractKeywords(longPrompt))); count > 12 {
			t.Fatalf("expected at most 12 terms, got %d", count)
		}
	})

	t.Run("handles empty input", func(t *testing.T) {
		if got := ExtractKeywords(""); got != "" {
			t.Fatalf("got %q", got)
		}
		if got := ExtractKeywords("the a an"); got != "" {
			t.Fatalf("got %q", got)
		}
	})
}

func TestPreprocessQuery(t *testing.T) {
	query := "I want to extract text from a PDF"
	if got := preprocessQuery(query, "raw"); got != query {
		t.Fatalf("got %q", got)
	}
	if got := preprocessQuery(query, "keywords"); got != "extract text pdf" {
		t.Fatalf("got %q", got)
	}
}
