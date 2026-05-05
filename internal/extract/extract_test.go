package extract

import (
	"testing"
)

func TestGoExtractor(t *testing.T) {
	ex := &GoExtractor{}
	result, err := ex.Extract("testdata/fixtures/go/simple/main.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) == 0 {
		t.Fatal("expected nodes")
	}
	if len(result.Edges) == 0 {
		t.Fatal("expected edges")
	}
}
