package codemogger

import "sort"

func rrfMerge(ftsResults, vecResults []SearchResult, limit, k int, ftsWeight, vecWeight float64) []SearchResult {
	if k == 0 {
		k = 60
	}
	if ftsWeight == 0 {
		ftsWeight = 0.4
	}
	if vecWeight == 0 {
		vecWeight = 0.6
	}

	scores := make(map[string]float64, len(ftsResults)+len(vecResults))
	data := make(map[string]SearchResult, len(ftsResults)+len(vecResults))

	for i, result := range ftsResults {
		scores[result.ChunkKey] += ftsWeight / float64(k+i+1)
		data[result.ChunkKey] = result
	}

	for i, result := range vecResults {
		scores[result.ChunkKey] += vecWeight / float64(k+i+1)
		if _, ok := data[result.ChunkKey]; !ok {
			data[result.ChunkKey] = result
		}
	}

	keys := make([]string, 0, len(scores))
	for key := range scores {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return scores[keys[i]] > scores[keys[j]]
	})
	if limit > 0 && len(keys) > limit {
		keys = keys[:limit]
	}

	out := make([]SearchResult, 0, len(keys))
	for _, key := range keys {
		row := data[key]
		row.Score = scores[key]
		out = append(out, row)
	}
	return out
}
