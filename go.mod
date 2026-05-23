module github.com/julianshen/gogfy

go 1.26

require (
	github.com/fsnotify/fsnotify v1.10.1
	github.com/sabhiram/go-gitignore v0.0.0-20210923224102-525f6e181f06
	github.com/tree-sitter-grammars/tree-sitter-kotlin v1.1.0
	github.com/tree-sitter-grammars/tree-sitter-lua v0.5.0
	github.com/tree-sitter-grammars/tree-sitter-svelte v1.0.2
	github.com/tree-sitter-grammars/tree-sitter-toml v0.7.0
	github.com/tree-sitter-grammars/tree-sitter-yaml v0.7.2
	github.com/tree-sitter-grammars/tree-sitter-zig v1.1.2
	github.com/tree-sitter/go-tree-sitter v0.25.0
	github.com/tree-sitter/tree-sitter-bash v0.25.1
	github.com/tree-sitter/tree-sitter-c v0.24.2
	github.com/tree-sitter/tree-sitter-c-sharp v0.23.5
	github.com/tree-sitter/tree-sitter-cpp v0.23.4
	github.com/tree-sitter/tree-sitter-go v0.25.0
	github.com/tree-sitter/tree-sitter-haskell v0.23.1
	github.com/tree-sitter/tree-sitter-java v0.23.5
	github.com/tree-sitter/tree-sitter-javascript v0.25.0
	github.com/tree-sitter/tree-sitter-julia v0.25.0
	github.com/tree-sitter/tree-sitter-ocaml v0.25.0
	github.com/tree-sitter/tree-sitter-php v0.24.2
	github.com/tree-sitter/tree-sitter-python v0.25.0
	github.com/tree-sitter/tree-sitter-ruby v0.23.1
	github.com/tree-sitter/tree-sitter-rust v0.24.2
	github.com/tree-sitter/tree-sitter-scala v0.26.0
	github.com/tree-sitter/tree-sitter-typescript v0.23.2
	github.com/vsuryav/leiden-go v0.0.0-20251120005855-0f56599dc139
)

require (
	dario.cat/mergo v1.0.0 // indirect
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/ProtonMail/go-crypto v1.1.6 // indirect
	github.com/UserNobody14/tree-sitter-dart v0.0.0-20260508020638-507c5546dc73 // indirect
	github.com/bitly/go-simplejson v0.5.1 // indirect
	github.com/cloudflare/circl v1.6.3 // indirect
	github.com/cyphar/filepath-securejoin v0.6.1 // indirect
	github.com/dgryski/go-minhash v0.0.0-20190315135803-ad340ca03076 // indirect
	github.com/dgryski/go-spooky v0.0.0-20170606183049-ed3d087f40e2 // indirect
	github.com/dlclark/regexp2 v1.11.5 // indirect
	github.com/dop251/goja v0.0.0-20260311135729-065cd970411c // indirect
	github.com/emirpasic/gods v1.18.1 // indirect
	github.com/go-git/gcfg v1.5.1-0.20230307220236-3a3c6141e376 // indirect
	github.com/go-git/go-billy/v5 v5.9.0 // indirect
	github.com/go-git/go-git/v5 v5.19.1 // indirect
	github.com/go-sourcemap/sourcemap v2.1.4+incompatible // indirect
	github.com/golang/groupcache v0.0.0-20241129210726-2c02b8208cf8 // indirect
	github.com/google/pprof v0.0.0-20260302011040-a15ffb7f9dcc // indirect
	github.com/jbenet/go-context v0.0.0-20150711004518-d14ea06fba99 // indirect
	github.com/julianshen/tree-sitter-elixir v0.3.6 // indirect
	github.com/julianshen/tree-sitter-erlang v0.0.0-20260510145358-80fab4f46bdf // indirect
	github.com/julianshen/tree-sitter-r v1.2.1-0.20260510134556-9a03ef5f3473 // indirect
	github.com/julianshen/tree-sitter-swift v0.0.0-20260510074952-3d85c1637e38 // indirect
	github.com/kevinburke/ssh_config v1.2.0 // indirect
	github.com/kkdai/youtube/v2 v2.10.6 // indirect
	github.com/klauspost/cpuid/v2 v2.3.0 // indirect
	github.com/ledongthuc/pdf v0.0.0-20250511090121-5959a4027728 // indirect
	github.com/mattn/go-pointer v0.0.1 // indirect
	github.com/neo4j/neo4j-go-driver/v5 v5.28.4 // indirect
	github.com/pjbgf/sha1cd v0.6.0 // indirect
	github.com/sergi/go-diff v1.3.2-0.20230802210424-5b0b94c5c0d3 // indirect
	github.com/skeema/knownhosts v1.3.1 // indirect
	github.com/stadelmanma/tree-sitter-fortran v0.6.0 // indirect
	github.com/xanzy/ssh-agent v0.3.3 // indirect
	github.com/xrash/smetrics v0.0.0-20250705151800-55b8f293f342 // indirect
	github.com/yuin/goldmark v1.8.2 // indirect
	golang.org/x/crypto v0.51.0 // indirect
	golang.org/x/net v0.54.0 // indirect
	golang.org/x/sys v0.44.0 // indirect
	golang.org/x/text v0.37.0 // indirect
	gopkg.in/warnings.v0 v0.1.2 // indirect
)

// No `replace` directives: gogfy is installable via
// `go install github.com/julianshen/gogfy/cmd/gogfy@latest` (Go forbids
// modules consumed at @latest from carrying replace directives).
//
// The Elixir grammar is consumed via the github.com/julianshen/tree-sitter-elixir
// fork, which declares the correct module path in its own go.mod (upstream
// elixir-lang/tree-sitter-elixir declares a non-existent
// github.com/tree-sitter/tree-sitter-elixir path).
//
// The Fortran grammar is imported directly from the stadelmanma path —
// its upstream test files reference a non-existent path, but that only
// affects `go mod tidy` chains over its test transitives, not gogfy's
// own build or `go install` path.
