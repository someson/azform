package ui

import "strings"

// FilterFields returns the indices (into fields) of params whose name or help
// text contains query (case-insensitive). An empty query returns all indices.
func FilterFields(fields []Field, query string) []int {
	if query == "" {
		all := make([]int, len(fields))
		for i := range fields {
			all[i] = i
		}
		return all
	}
	q := strings.ToLower(query)
	var result []int
	for i, f := range fields {
		if matchesFilter(f, q) {
			result = append(result, i)
		}
	}
	return result
}

// matchesFilter reports whether a single field's name or help text contains
// the (already-lowercased) query. Extracted from FilterFields so the
// single-column renderer can re-apply the same predicate to required rows
// that the bulk filter would otherwise bypass (the renderer iterates
// reqIndices directly rather than the filtered visible list).
func matchesFilter(f Field, lowerQuery string) bool {
	if lowerQuery == "" {
		return true
	}
	name := strings.ToLower(f.Param.Name)
	help := strings.ToLower(f.Param.Help)
	return strings.Contains(name, lowerQuery) || strings.Contains(help, lowerQuery)
}
