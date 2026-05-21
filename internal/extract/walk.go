package extract

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"unsafe"

	"github.com/julianshen/gogfy/internal/schema"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// ParseErrorLogger receives stderr-bound warnings about files that produced
// parser errors (tree-sitter ERROR/MISSING nodes). Tests override it to
// assert; production leaves the default. Set to io.Discard to silence.
//
// Tree-sitter's HasError can occasionally false-positive on grammar edge
// cases (some Go generics shapes, partial templated YAML, etc.) — the
// warning is advisory and never fails the build.
var ParseErrorLogger io.Writer = os.Stderr

// warnIfParseError emits one stderr line when the parsed tree has any ERROR
// or MISSING nodes. Centralized so per-extractor parse paths can call this
// without duplicating the format string (and without double-firing when an
// extractor uses both its own parser AND parseFile).
func warnIfParseError(absPath string, tree *sitter.Tree) {
	if tree.RootNode().HasError() {
		fmt.Fprintf(ParseErrorLogger,
			"gogfy: parse warning: %s contains syntax errors; extraction may be incomplete\n",
			absPath)
	}
}

// parsedFile bundles the tree-sitter parse output and source bytes for one file,
// along with a single cleanup func that closes the parser/tree/cursor.
type parsedFile struct {
	src     []byte
	absPath string
	cursor  *sitter.TreeCursor
	cleanup func()
}

// runExtraction is the shared scaffold every simple extractor follows: parse
// the file, emit the file-as-module node, run the per-language walker, return
// the accumulated Result. Languages with extra walker arguments (e.g., YAML's
// mappingDepth) wrap walk in a closure.
func runExtraction(path string, lang unsafe.Pointer, langID string, walk func(*sitter.TreeCursor, []byte, *extractState)) (Result, error) {
	pf, err := parseFile(path, lang)
	if err != nil {
		return Result{}, err
	}
	defer pf.cleanup()
	state := &extractState{lang: langID, filePath: pf.absPath}
	state.emitModule(pf.cursor.Node())
	walk(pf.cursor, pf.src, state)
	state.finalize()
	return Result{Nodes: state.nodes, Edges: state.edges}, nil
}

// parseFile reads path, parses with the given grammar, and returns a parsedFile
// the caller must Close via cleanup.
func parseFile(path string, lang unsafe.Pointer) (*parsedFile, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	parser := sitter.NewParser()
	if err := parser.SetLanguage(sitter.NewLanguage(lang)); err != nil {
		parser.Close()
		return nil, err
	}
	tree := parser.Parse(src, nil)
	warnIfParseError(abs, tree)
	cursor := tree.Walk()
	return &parsedFile{
		src:     src,
		absPath: abs,
		cursor:  cursor,
		cleanup: func() {
			cursor.Close()
			tree.Close()
			parser.Close()
		},
	}, nil
}

// nodeLocation formats a node's start position, calling StartPosition only once.
func nodeLocation(n *sitter.Node) string {
	p := n.StartPosition()
	return schema.FormatLocation(p.Row, p.Column)
}

// walkChildren visits each child of the cursor's current node and restores
// the cursor to that node before returning. visit MUST leave the cursor on
// the same node it was called with — moving the cursor inside visit will
// silently desync the iteration.
func walkChildren(cursor *sitter.TreeCursor, visit func()) {
	if !cursor.GotoFirstChild() {
		return
	}
	for {
		visit()
		if !cursor.GotoNextSibling() {
			break
		}
	}
	cursor.GotoParent()
}
