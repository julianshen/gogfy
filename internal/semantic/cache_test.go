package semantic

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/julianshen/gogfy/internal/llm"
	"github.com/julianshen/gogfy/internal/schema"
)

// fakeClient counts Generate calls so we can assert the cache
// short-circuits the LLM on hits.
type fakeClient struct {
	calls int
	resp  string
	err   error
}

func (f *fakeClient) Name() string { return "fake-1" }
func (f *fakeClient) Generate(ctx context.Context, req llm.Request) (llm.Response, error) {
	f.calls++
	if f.err != nil {
		return llm.Response{}, f.err
	}
	return llm.Response{Text: f.resp, InputTokens: 10, OutputTokens: 20, EstimatedUSDCost: 0.001}, nil
}

const validResponse = `{"entities":[{"id":"py:class:/a.py:Foo","label":"Foo","type":"class"}],"relations":[]}`

func TestExtractCachedHitSkipsLLMCall(t *testing.T) {
	dir := t.TempDir()
	cache := NewCache(dir)
	c := &fakeClient{resp: validResponse}
	src := []byte("# a markdown doc with content\n")

	// First call: cache miss → LLM call, result stored.
	r1, err := ExtractCached(context.Background(), c, "/a.md", src, cache)
	if err != nil {
		t.Fatal(err)
	}
	if c.calls != 1 {
		t.Fatalf("expected 1 LLM call on first run, got %d", c.calls)
	}
	if len(r1.Nodes) == 0 {
		t.Fatalf("first call returned no nodes: %+v", r1)
	}

	// Second call with same src: must hit cache, no LLM call.
	r2, err := ExtractCached(context.Background(), c, "/a.md", src, cache)
	if err != nil {
		t.Fatal(err)
	}
	if c.calls != 1 {
		t.Errorf("cache hit should NOT call LLM again, got %d total calls", c.calls)
	}
	if len(r2.Nodes) != len(r1.Nodes) {
		t.Errorf("cached result lost nodes: first=%d second=%d", len(r1.Nodes), len(r2.Nodes))
	}
}

func TestCacheKeyIgnoresMarkdownFrontmatter(t *testing.T) {
	// `kind: tweet` vs `kind: github` frontmatter is metadata, not
	// content — the semantic extraction depends on the body, so
	// swapping frontmatter must hit the same cache slot.
	a := []byte("---\nkind: tweet\nsource_url: \"x\"\n---\n\nhello world\n")
	b := []byte("---\nkind: github\nsource_url: \"y\"\n---\n\nhello world\n")
	ka := CacheKey("c1", systemPrompt, "text", a)
	kb := CacheKey("c1", systemPrompt, "text", b)
	if ka != kb {
		t.Errorf("frontmatter difference busted cache: %q vs %q", ka, kb)
	}
}

func TestCacheKeyDistinguishesByClientName(t *testing.T) {
	// Switching backends (anthropic → openai) must NOT reuse cached
	// results — different models produce different outputs.
	src := []byte("body\n")
	a := CacheKey("anthropic-claude", systemPrompt, "text", src)
	b := CacheKey("openai-gpt4", systemPrompt, "text", src)
	if a == b {
		t.Errorf("client switch should change cache key: both %q", a)
	}
}

func TestCacheKeyDistinguishesTextFromImage(t *testing.T) {
	// A path that happens to have identical bytes in text vs image
	// mode (improbable but possible) must NOT share a cache slot.
	src := []byte("\x89PNG\r\n\x1a\nx")
	a := CacheKey("c1", systemPrompt, "text", src)
	b := CacheKey("c1", systemPrompt, "image", src)
	if a == b {
		t.Errorf("text/image mode should fork cache key: both %q", a)
	}
}

func TestCacheCorruptEntryTreatedAsMiss(t *testing.T) {
	// A truncated / hand-edited cache file should NOT propagate as
	// an error — treat it as a miss, rewrite on the next success.
	dir := t.TempDir()
	cache := NewCache(dir)
	src := []byte("body\n")
	key := CacheKey("fake-1", systemPrompt, "text", src)
	// Plant a corrupt entry.
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, key+".json"), []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}

	c := &fakeClient{resp: validResponse}
	r, err := ExtractCached(context.Background(), c, "/a.md", src, cache)
	if err != nil {
		t.Fatal(err)
	}
	if c.calls != 1 {
		t.Errorf("corrupt entry should be treated as a miss → LLM called once; got %d", c.calls)
	}
	if len(r.Nodes) == 0 {
		t.Errorf("expected nodes from fresh extraction, got empty result")
	}
}

func TestExtractCachedDoesNotStoreOnLLMError(t *testing.T) {
	// If the LLM call errors, the cache must not store a result —
	// otherwise the failed attempt would silently shadow future
	// healthy responses.
	dir := t.TempDir()
	cache := NewCache(dir)
	c := &fakeClient{err: errors.New("boom")}
	src := []byte("body\n")
	if _, err := ExtractCached(context.Background(), c, "/a.md", src, cache); err == nil {
		t.Errorf("expected LLM error to propagate")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("cache should be empty after a failed call, got %d entries", len(entries))
	}
}

func TestCacheNilSafeForLookupAndStore(t *testing.T) {
	// A nil *Cache must be safe to pass through — that's the
	// signal callers use to disable caching without threading bool
	// flags through the call stack.
	var c *Cache
	if _, ok := c.Lookup("x"); ok {
		t.Errorf("nil cache must miss")
	}
	if err := c.Store("x", Result{}); err != nil {
		t.Errorf("nil cache Store should be a no-op, got %v", err)
	}
}

// Sanity: the cache round-trips schema.Node and schema.Edge correctly
// (including non-default fields).
func TestCacheRoundTripsSchemaFields(t *testing.T) {
	dir := t.TempDir()
	cache := NewCache(dir)
	want := Result{
		Nodes: []schema.Node{{ID: "py:class:/a:Foo", Label: "Foo", FileType: "code"}},
		Edges: []schema.Edge{{Source: "py:module:/a", Target: "py:class:/a:Foo", Relation: "contains", Confidence: schema.Extracted}},
		InputTokens:      5,
		OutputTokens:     7,
		EstimatedUSDCost: 0.0001,
	}
	if err := cache.Store("k", want); err != nil {
		t.Fatal(err)
	}
	got, ok := cache.Lookup("k")
	if !ok {
		t.Fatal("cache miss after store")
	}
	if len(got.Nodes) != 1 || got.Nodes[0].FileType != "code" {
		t.Errorf("node FileType lost: %+v", got.Nodes)
	}
	if got.Edges[0].Confidence != schema.Extracted {
		t.Errorf("edge Confidence lost: %v", got.Edges[0].Confidence)
	}
	if got.InputTokens != 5 || got.OutputTokens != 7 {
		t.Errorf("token counts drifted: %+v", got)
	}
}
