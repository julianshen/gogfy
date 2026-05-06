package schema

import (
	"fmt"
	"sort"
)

// FormatLocation formats a tree-sitter row:col into a human-readable location string.
func FormatLocation(row, col uint32) string {
	return fmt.Sprintf("%d:%d", row+1, col+1)
}

// PackageID returns a deterministic node ID for a package.
func PackageID(filePath, pkgName string) string {
	return fmt.Sprintf("pkg:%s:%s", filePath, pkgName)
}

// FuncID returns a deterministic node ID for a function.
func FuncID(filePath, pkgName, funcName string) string {
	return fmt.Sprintf("fn:%s:%s.%s", filePath, pkgName, funcName)
}

// ImportID returns a deterministic node ID for an imported package.
func ImportID(imp string) string {
	return fmt.Sprintf("pkg:import:%s", imp)
}

// SortNodesByID sorts nodes in-place by ID for deterministic output.
func SortNodesByID(nodes []Node) {
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].ID < nodes[j].ID
	})
}
