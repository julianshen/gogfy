package semantic

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/julianshen/gogfy/internal/fsutil"
	"github.com/julianshen/gogfy/internal/schema"
)

// Cache memoizes Result values by a content-derived key so re-runs
// of `gogfy run --semantic` skip the LLM call on files whose content
// + extraction parameters haven't changed.
//
// Layout: one JSON file per cache entry under <Dir>/<key>.json. The
// per-file format gives us cheap atomicity (rename-on-write), trivial
// invalidation (delete file), and corruption recovery (a single
// unreadable entry is treated as a miss, not as a corpus-wide cache
// poison).
//
// The cache is keyed on:
//   - the LLM client's Name() (so switching backends doesn't reuse
//     anthropic's output for openai's call)
//   - the system prompt (so a prompt revision invalidates everything)
//   - the file's content with frontmatter stripped (so swapping a
//     markdown sidecar's YAML metadata doesn't bust the cache)
//   - "image" vs "text" mode marker (so ExtractImage and Extract
//     never collide on a path that's somehow both)
type Cache struct {
	dir string
}

// NewCache returns a Cache rooted at dir. The directory is created on
// first Store call (lazy) so a Lookup against a never-populated cache
// doesn't create empty directories.
func NewCache(dir string) *Cache { return &Cache{dir: dir} }

// Lookup returns the cached Result for key and whether one exists.
// Returns (zero, false) on any error — corruption-tolerant by design,
// because a single bad entry shouldn't force a full re-extract.
func (c *Cache) Lookup(key string) (Result, bool) {
	if c == nil || c.dir == "" {
		return Result{}, false
	}
	data, err := os.ReadFile(filepath.Join(c.dir, key+".json"))
	if err != nil {
		return Result{}, false
	}
	var cached cacheEntry
	if err := json.Unmarshal(data, &cached); err != nil {
		// Corrupt entry — silently treat as miss. Caller will rewrite
		// it on success, replacing the bad bytes.
		return Result{}, false
	}
	return Result{
		Nodes:            cached.Nodes,
		Edges:            cached.Edges,
		InputTokens:      cached.InputTokens,
		OutputTokens:     cached.OutputTokens,
		EstimatedUSDCost: cached.EstimatedUSDCost,
	}, true
}

// Store writes the result under key. Atomic write via fsutil so a
// crashed run doesn't leave a half-flushed JSON file (which would
// then look corrupt and re-trigger the LLM next time).
func (c *Cache) Store(key string, r Result) error {
	if c == nil || c.dir == "" {
		return nil
	}
	if err := os.MkdirAll(c.dir, 0755); err != nil {
		return fmt.Errorf("semantic cache: mkdir %s: %w", c.dir, err)
	}
	data, err := json.Marshal(cacheEntry{
		Nodes:            r.Nodes,
		Edges:            r.Edges,
		InputTokens:      r.InputTokens,
		OutputTokens:     r.OutputTokens,
		EstimatedUSDCost: r.EstimatedUSDCost,
	})
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(filepath.Join(c.dir, key+".json"), data, 0644)
}

// CacheKey computes the deterministic cache key for an extraction.
// Exposed so tests can pin the keying scheme — a regression here
// would silently invalidate every existing cache entry on the next
// run, manifesting as "everything costs LLM money again" with no
// other symptom.
func CacheKey(clientName, prompt, mode string, src []byte) string {
	stripped := src
	if mode == "text" {
		stripped = stripMarkdownFrontmatter(src)
	}
	h := sha256.New()
	// Lengths are part of the digest so two of {prompt, src} can't
	// alias by adversarial concatenation.
	fmt.Fprintf(h, "%d|%s|%d|%s|%d|", len(clientName), clientName, len(prompt), prompt, len(mode))
	h.Write([]byte(mode))
	h.Write([]byte{0})
	h.Write(stripped)
	return hex.EncodeToString(h.Sum(nil))
}

// stripMarkdownFrontmatter removes a leading YAML frontmatter block
// (`---\n…\n---\n`) before hashing. The ingest layer prepends source
// metadata that isn't part of the content's semantic signal —
// swapping `kind: tweet` for `kind: github` on the same body should
// not invalidate the cache.
func stripMarkdownFrontmatter(src []byte) []byte {
	const marker = "---\n"
	if !strings.HasPrefix(string(src), marker) {
		return src
	}
	end := strings.Index(string(src[len(marker):]), "\n---\n")
	if end < 0 {
		return src
	}
	return src[len(marker)+end+len("\n---\n"):]
}

// cacheEntry is the on-disk shape. Kept separate from Result so a
// future schema-evolution (e.g. adding a model-version field) doesn't
// require a Result struct change.
type cacheEntry struct {
	Nodes            []schema.Node `json:"nodes"`
	Edges            []schema.Edge `json:"edges"`
	InputTokens      int           `json:"input_tokens"`
	OutputTokens     int           `json:"output_tokens"`
	EstimatedUSDCost float64       `json:"estimated_usd_cost"`
}

// ErrCacheMiss isn't returned by Lookup (which returns a bool) — it
// exists so callers integrating the cache into a fall-through chain
// have a sentinel for "skipped because nothing was cached", distinct
// from real errors. Currently unused; kept for API symmetry with the
// other cache-bearing packages.
var ErrCacheMiss = errors.New("semantic cache: miss")
