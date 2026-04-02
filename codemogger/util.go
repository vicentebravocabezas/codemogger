package codemogger

import (
	"math"
	"path/filepath"
	"strings"
)

func toLower(s string) string {
	return strings.ToLower(s)
}

func joinWithSpace(parts []string) string {
	return strings.Join(parts, " ")
}

func normalizeLanguageNames(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		out[strings.ToLower(strings.TrimSpace(value))] = struct{}{}
	}
	return out
}

func baseName(path string) string {
	return filepath.Base(path)
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot float64
	var normA float64
	var normB float64
	for i := range a {
		af := float64(a[i])
		bf := float64(b[i])
		dot += af * bf
		normA += af * af
		normB += bf * bf
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
