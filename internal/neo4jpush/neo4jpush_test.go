package neo4jpush

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/julianshen/gogfy/internal/export"
)

// resolveOptions is the only part of the package that's testable
// without a live Neo4j instance. Real push tests need an integration
// harness (testcontainers / docker compose) and live outside CI.

func TestResolveOptionsRequiresURI(t *testing.T) {
	t.Setenv("NEO4J_URI", "")
	_, err := resolveOptions(&Options{})
	if !errors.Is(err, ErrMissingURI) {
		t.Errorf("expected ErrMissingURI, got %v", err)
	}
}

func TestResolveOptionsFallsBackToEnv(t *testing.T) {
	t.Setenv("NEO4J_URI", "neo4j://example.com:7687")
	t.Setenv("NEO4J_USER", "alice")
	t.Setenv("NEO4J_PASSWORD", "p")
	t.Setenv("NEO4J_DATABASE", "graphdb")
	out, err := resolveOptions(&Options{})
	if err != nil {
		t.Fatal(err)
	}
	if out.URI != "neo4j://example.com:7687" {
		t.Errorf("URI: %q", out.URI)
	}
	if out.Username != "alice" {
		t.Errorf("Username: %q", out.Username)
	}
	if out.Password != "p" {
		t.Errorf("Password: %q", out.Password)
	}
	if out.Database != "graphdb" {
		t.Errorf("Database: %q", out.Database)
	}
}

func TestResolveOptionsExplicitOverridesEnv(t *testing.T) {
	// Explicit Options values must win over env — CLI flags are the
	// user's direct intent and shouldn't be shadowed by stale env.
	t.Setenv("NEO4J_URI", "neo4j://env-default:7687")
	t.Setenv("NEO4J_USER", "env-user")
	out, err := resolveOptions(&Options{URI: "bolt://explicit:7687", Username: "explicit-user"})
	if err != nil {
		t.Fatal(err)
	}
	if out.URI != "bolt://explicit:7687" {
		t.Errorf("explicit URI lost: %q", out.URI)
	}
	if out.Username != "explicit-user" {
		t.Errorf("explicit Username lost: %q", out.Username)
	}
}

func TestResolveOptionsDefaultsUsernameToNeo4j(t *testing.T) {
	t.Setenv("NEO4J_URI", "bolt://localhost:7687")
	t.Setenv("NEO4J_USER", "")
	out, _ := resolveOptions(&Options{})
	if out.Username != "neo4j" {
		t.Errorf("expected default username 'neo4j', got %q", out.Username)
	}
}

func TestResolveOptionsDefaultsBatchSize(t *testing.T) {
	t.Setenv("NEO4J_URI", "bolt://localhost:7687")
	out, _ := resolveOptions(&Options{})
	if out.BatchSize != 500 {
		t.Errorf("default batch size: got %d want 500", out.BatchSize)
	}
}

func TestPushReturnsURIErrorBeforeDriverConnect(t *testing.T) {
	// No URI → fail fast before opening any network connection. We
	// don't want the driver to spin up only to be told there's no
	// host to talk to.
	t.Setenv("NEO4J_URI", "")
	_, err := Push(context.Background(), export.GraphExport{}, nil)
	if !errors.Is(err, ErrMissingURI) {
		t.Errorf("expected ErrMissingURI from Push when URI unset, got %v", err)
	}
}

func TestCypherRelTypeMirrorsExportPackage(t *testing.T) {
	// neo4jpush.cypherRelType is a duplicate of export.cypherRelType
	// (avoid import cycle). Pin the sanitization rules so a future
	// change to one doesn't silently diverge from the other —
	// downstream consumers expect both paths produce the same
	// relationship-type strings.
	cases := map[string]string{
		"calls":        "CALLS",
		"semantically_similar_to": "SEMANTICALLY_SIMILAR_TO",
		"rationale_for": "RATIONALE_FOR",
		"":             "RELATED",
		"123starts":    "_123STARTS",
		"with-dash":    "WITH_DASH",
		"with space":   "WITH_SPACE",
	}
	for in, want := range cases {
		if got := cypherRelType(in); got != want {
			t.Errorf("cypherRelType(%q) = %q, want %q", in, got, want)
		}
	}
}

// Smoke: with explicit Options the resolver fills in everything
// even when NEO4J_* env is completely empty.
func TestResolveOptionsAllExplicitNoEnv(t *testing.T) {
	t.Setenv("NEO4J_URI", "")
	t.Setenv("NEO4J_USER", "")
	t.Setenv("NEO4J_PASSWORD", "")
	t.Setenv("NEO4J_DATABASE", "")
	out, err := resolveOptions(&Options{
		URI: "bolt://h:7687", Username: "u", Password: "p", Database: "d", BatchSize: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.URI != "bolt://h:7687" || out.Username != "u" || out.Password != "p" || out.Database != "d" || out.BatchSize != 100 {
		t.Errorf("env-less path didn't preserve explicit options: %+v", out)
	}
	if strings.Contains(out.Database, "/") {
		t.Errorf("database name shouldn't have a slash")
	}
}
