// Package schema defines the core data types for the gogfy graph model.
package schema

import (
	"fmt"
	"sort"
)

// FormatLocation formats a tree-sitter row:col (zero-based) into a 1-based "row:col" string.
func FormatLocation(row, col uint) string {
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

// PythonModuleID returns a deterministic node ID for a Python module.
func PythonModuleID(filePath string) string {
	return fmt.Sprintf("py:module:%s", filePath)
}

// PythonFuncID returns a deterministic node ID for a Python function.
func PythonFuncID(filePath, funcName string) string {
	return fmt.Sprintf("py:fn:%s:%s", filePath, funcName)
}

// PythonClassID returns a deterministic node ID for a Python class.
func PythonClassID(filePath, className string) string {
	return fmt.Sprintf("py:class:%s:%s", filePath, className)
}

// PythonImportID returns a deterministic node ID for a Python imported module.
func PythonImportID(imp string) string {
	return fmt.Sprintf("py:import:%s", imp)
}

// SortNodesByID sorts nodes in-place by ID for deterministic output.
func SortNodesByID(nodes []Node) {
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].ID < nodes[j].ID
	})
}
