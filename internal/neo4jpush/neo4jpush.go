// Package neo4jpush pushes a GraphExport directly to a running Neo4j
// instance via the Bolt protocol. Complements the existing file-based
// Cypher export (internal/export.ExportCypher) — both produce
// equivalent MERGE-based idempotent writes; this one skips the
// `cypher-shell -f` round trip.
//
// Auth flows through the standard NEO4J_* env vars with CLI overrides:
//
//	NEO4J_URI       e.g. neo4j://localhost:7687, neo4j+s://aura.example
//	NEO4J_USER      defaults to "neo4j"
//	NEO4J_PASSWORD  required (no default — empty string is a real
//	                  credential in some test setups)
//	NEO4J_DATABASE  optional; empty = the server's default db
//
// Writes are batched in transactions: nodes first (so subsequent
// edges can MATCH them), then edges. Batch size is tuned for the
// driver's default packet buffer; per-write transaction overhead
// dominates without batching for graphs above a few hundred nodes.
package neo4jpush

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/julianshen/gogfy/internal/export"
	"github.com/julianshen/gogfy/internal/schema"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// Options configures the push. Zero values fall back to env / sane
// defaults so a minimal call site is just neo4jpush.Push(ctx, g, nil).
type Options struct {
	URI      string // overrides NEO4J_URI
	Username string // overrides NEO4J_USER (default "neo4j")
	Password string // overrides NEO4J_PASSWORD
	Database string // overrides NEO4J_DATABASE (empty = server default)
	// BatchSize controls how many nodes/edges per transaction. Larger
	// reduces round-trips at the cost of larger memory peaks. Default
	// 500 — tuned against neo4j-go-driver's default chunk buffer.
	BatchSize int
}

// ErrMissingURI fires when no URI is provided via Options.URI or
// NEO4J_URI. We don't default to localhost because a typo (e.g. an
// unset env var when the user expected one) shouldn't silently push
// to a developer-local instance.
var ErrMissingURI = errors.New("neo4jpush: NEO4J_URI environment variable or Options.URI required")

// Push opens a Bolt session and writes the graph using MERGE
// statements idempotent on node ID. Returns the count of writes (for
// the CLI's progress line) and an error if any transaction failed.
func Push(ctx context.Context, g export.GraphExport, opts *Options) (int, error) {
	if opts == nil {
		opts = &Options{}
	}
	resolved, err := resolveOptions(opts)
	if err != nil {
		return 0, err
	}
	driver, err := neo4j.NewDriverWithContext(resolved.URI, neo4j.BasicAuth(resolved.Username, resolved.Password, ""))
	if err != nil {
		return 0, fmt.Errorf("neo4jpush: driver: %w", err)
	}
	defer driver.Close(ctx)
	// Verify the connection up front so a typo in URI surfaces here
	// rather than mid-batch with half the nodes already written.
	if err := driver.VerifyConnectivity(ctx); err != nil {
		return 0, fmt.Errorf("neo4jpush: connect: %w", err)
	}
	session := driver.NewSession(ctx, neo4j.SessionConfig{
		DatabaseName: resolved.Database,
		AccessMode:   neo4j.AccessModeWrite,
	})
	defer session.Close(ctx)

	writes := 0
	w, err := pushNodes(ctx, session, g.Nodes, resolved.BatchSize)
	if err != nil {
		return writes, err
	}
	writes += w
	w, err = pushEdges(ctx, session, g.Edges, resolved.BatchSize)
	if err != nil {
		return writes, err
	}
	writes += w
	return writes, nil
}

// pushNodes writes each node as MERGE (n:Node {id: $id}) SET ...
// in batched transactions. Properties match the file-based Cypher
// export (label, source_file, community, file_type) so consumers
// see identical graph state regardless of which path was used.
func pushNodes(ctx context.Context, session neo4j.SessionWithContext, nodes []schema.Node, batchSize int) (int, error) {
	sorted := append([]schema.Node(nil), nodes...)
	schema.SortNodesByID(sorted)
	written := 0
	for i := 0; i < len(sorted); i += batchSize {
		end := i + batchSize
		if end > len(sorted) {
			end = len(sorted)
		}
		batch := sorted[i:end]
		_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
			for _, n := range batch {
				params := map[string]any{"id": n.ID}
				setClauses := []string{}
				if n.Label != "" {
					params["label"] = n.Label
					setClauses = append(setClauses, "n.label = $label")
				}
				if n.SourceFile != "" {
					params["source_file"] = n.SourceFile
					setClauses = append(setClauses, "n.source_file = $source_file")
				}
				if n.Community != "" {
					params["community"] = n.Community
					setClauses = append(setClauses, "n.community = $community")
				}
				if n.FileType != "" {
					params["file_type"] = string(n.FileType)
					setClauses = append(setClauses, "n.file_type = $file_type")
				}
				query := "MERGE (n:Node {id: $id})"
				if len(setClauses) > 0 {
					query += " SET " + strings.Join(setClauses, ", ")
				}
				if _, err := tx.Run(ctx, query, params); err != nil {
					return nil, err
				}
			}
			return nil, nil
		})
		if err != nil {
			return written, fmt.Errorf("neo4jpush: write nodes batch %d-%d: %w", i, end, err)
		}
		written += len(batch)
	}
	return written, nil
}

// pushEdges writes each edge as MATCH ... MERGE relationship in
// batched transactions. Relationship type is derived from the
// schema.Edge.Relation field via the same uppercase-and-sanitize
// rules as the file-based Cypher export.
func pushEdges(ctx context.Context, session neo4j.SessionWithContext, edges []schema.Edge, batchSize int) (int, error) {
	sorted := append([]schema.Edge(nil), edges...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Source != sorted[j].Source {
			return sorted[i].Source < sorted[j].Source
		}
		if sorted[i].Target != sorted[j].Target {
			return sorted[i].Target < sorted[j].Target
		}
		return sorted[i].Relation < sorted[j].Relation
	})
	written := 0
	for i := 0; i < len(sorted); i += batchSize {
		end := i + batchSize
		if end > len(sorted) {
			end = len(sorted)
		}
		batch := sorted[i:end]
		_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
			for _, e := range batch {
				// Relationship type can't be parameterized in Cypher
				// — must be inlined. Sanitize through cypherRelType
				// (mirrors export.cypherRelType) so non-identifier
				// chars in the schema relation name don't break the
				// query.
				rel := cypherRelType(e.Relation)
				query := fmt.Sprintf(
					"MATCH (a:Node {id: $src}), (b:Node {id: $tgt}) MERGE (a)-[r:%s]->(b) SET r.confidence = $conf",
					rel,
				)
				params := map[string]any{
					"src":  e.Source,
					"tgt":  e.Target,
					"conf": e.Confidence.String(),
				}
				if _, err := tx.Run(ctx, query, params); err != nil {
					return nil, err
				}
			}
			return nil, nil
		})
		if err != nil {
			return written, fmt.Errorf("neo4jpush: write edges batch %d-%d: %w", i, end, err)
		}
		written += len(batch)
	}
	return written, nil
}

// resolveOptions fills in defaults from env and validates required
// fields. Done in one pass so the call site doesn't have to remember
// the order (URI first, then username default, then password env
// fallback, then batch size).
func resolveOptions(opts *Options) (Options, error) {
	out := *opts
	if out.URI == "" {
		out.URI = os.Getenv("NEO4J_URI")
	}
	if out.URI == "" {
		return Options{}, ErrMissingURI
	}
	if out.Username == "" {
		out.Username = os.Getenv("NEO4J_USER")
	}
	if out.Username == "" {
		out.Username = "neo4j"
	}
	if out.Password == "" {
		out.Password = os.Getenv("NEO4J_PASSWORD")
	}
	// Password may legitimately be empty for unauthenticated dev
	// instances — don't error here; the connect call will surface
	// auth failure if the server requires it.
	if out.Database == "" {
		out.Database = os.Getenv("NEO4J_DATABASE")
	}
	if out.BatchSize <= 0 {
		out.BatchSize = 500
	}
	return out, nil
}

// cypherRelType mirrors export.cypherRelType — duplicated rather
// than imported to avoid a circular dependency (export imports
// schema but not this package; this package imports both). Kept in
// sync; tests verify byte-identical output.
func cypherRelType(rel string) string {
	if rel == "" {
		return "RELATED"
	}
	var b strings.Builder
	for _, r := range strings.ToUpper(rel) {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	if out == "" || (out[0] >= '0' && out[0] <= '9') {
		out = "_" + out
	}
	return out
}
