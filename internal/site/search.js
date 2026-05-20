// gogfy site — client-side search. No framework dependencies; the
// inverted index ships as search-index.json alongside this file.
//
// Search semantics: tokenize the query the same way the indexer
// does (lowercase, split on non-word chars, drop tokens <3 chars),
// then intersect the page-id sets for each token. Result ranking is
// "more tokens matched first", tie-broken by alphabetical title so
// re-runs of the same query produce stable output.
//
// Substring fallback: if a query token has no index match, we look
// for it as a literal substring of page titles. Lets "graphd" find
// "graphdiff" even though the indexer wouldn't have a "graphd"
// token (only full words land in the index).
//
// Security note: all DOM construction uses createElement + textContent
// rather than innerHTML, so even a hypothetically-untrusted entry in
// GOGFY_PAGES cannot inject markup. Defense in depth — gogfy itself
// produces these titles, but the site might be served with other
// content under the same origin (Pages, S3).

(function () {
  const $input = document.getElementById("q");
  const $results = document.getElementById("results");
  const pages = window.GOGFY_PAGES || [];

  let invIndex = null;
  function loadIndex() {
    if (invIndex) return Promise.resolve(invIndex);
    return fetch("search-index.json")
      .then((r) => r.json())
      .then((data) => {
        invIndex = data;
        return data;
      });
  }

  function tokenize(s) {
    return s
      .toLowerCase()
      .split(/[^\p{L}\p{N}]+/u)
      .filter((t) => t.length >= 3);
  }

  function search(query) {
    return loadIndex().then((idx) => {
      const tokens = tokenize(query);
      if (tokens.length === 0) {
        const q = query.trim().toLowerCase();
        if (!q) return [];
        return pages
          .map((p, i) => ({ i, score: p.title.toLowerCase().includes(q) ? 1 : 0 }))
          .filter((r) => r.score > 0)
          .map((r) => r.i);
      }
      const score = new Map();
      tokens.forEach((tok) => {
        const hits = idx[tok] || [];
        hits.forEach((pid) => score.set(pid, (score.get(pid) || 0) + 1));
        if (hits.length === 0) {
          pages.forEach((p, i) => {
            if (p.title.toLowerCase().includes(tok)) {
              score.set(i, (score.get(i) || 0) + 1);
            }
          });
        }
      });
      const ranked = [...score.entries()]
        .sort((a, b) => {
          if (b[1] !== a[1]) return b[1] - a[1];
          return pages[a[0]].title.localeCompare(pages[b[0]].title);
        })
        .map((e) => e[0])
        .slice(0, 50); // cap so a generic query doesn't dump the whole index
      return ranked;
    });
  }

  // Render via DOM methods only — no innerHTML so untrusted strings
  // in GOGFY_PAGES can't inject markup. Clear by replacing children
  // wholesale; cheaper than per-node removal at this list size.
  function render(pageIDs) {
    while ($results.firstChild) {
      $results.removeChild($results.firstChild);
    }
    pageIDs.forEach((i) => {
      const p = pages[i];
      const li = document.createElement("li");
      const a = document.createElement("a");
      a.href = "pages/" + p.html_file;
      a.textContent = p.title;
      li.appendChild(a);
      $results.appendChild(li);
    });
  }

  let debounceTimer = null;
  $input.addEventListener("input", () => {
    clearTimeout(debounceTimer);
    debounceTimer = setTimeout(() => {
      const q = $input.value.trim();
      if (!q) {
        render([]);
        return;
      }
      search(q).then(render);
    }, 120);
  });
})();
