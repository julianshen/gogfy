package schema

import "fmt"

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
	// Use bubble sort for small slices to avoid import overhead
	for i := 0; i < len(nodes); i++ {
		for j := i + 1; j < len(nodes); j++ {
			if nodes[j].ID < nodes[i].ID {
				nodes[i], nodes[j] = nodes[j], nodes[i]
			}
		}
	}
}
