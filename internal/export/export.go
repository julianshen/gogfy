package export

import (
	"encoding/json"
	"fmt"

	"github.com/julianshen/gogfy/internal/schema"
)

type GraphExport struct {
	Nodes []schema.Node `json:"nodes"`
	Edges []schema.Edge `json:"edges"`
}

func ExportJSON(g GraphExport) ([]byte, error) {
	return json.MarshalIndent(g, "", "  ")
}

func ExportHTML(g GraphExport) ([]byte, error) {
	return []byte(fmt.Sprintf("<html><body>Nodes: %d, Edges: %d</body></html>", len(g.Nodes), len(g.Edges))), nil
}
