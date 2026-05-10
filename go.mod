module github.com/julianshen/gogfy

go 1.24

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
	github.com/UserNobody14/tree-sitter-dart v0.0.0-20260508020638-507c5546dc73 // indirect
	github.com/mattn/go-pointer v0.0.1 // indirect
	github.com/stadelmanma/tree-sitter-fortran v0.6.0 // indirect
	github.com/tree-sitter/tree-sitter-elixir v0.0.0-00010101000000-000000000000 // indirect
	golang.org/x/sys v0.13.0 // indirect
)

// stadelmanma/tree-sitter-fortran's binding_test.go imports a non-existent
// "github.com/tree-sitter/tree-sitter-fortran" path. The replace makes
// `go mod tidy` resolvable. Production code uses the stadelmanma path
// directly in internal/extract/fortranextractor.go.
replace github.com/tree-sitter/tree-sitter-fortran => github.com/stadelmanma/tree-sitter-fortran v0.6.0

// elixir-lang/tree-sitter-elixir declares its own module path under
// "github.com/tree-sitter/tree-sitter-elixir" (a repo that doesn't exist),
// but ships a working bindings/go. The replace makes the declared path
// resolvable.
replace github.com/tree-sitter/tree-sitter-elixir => github.com/elixir-lang/tree-sitter-elixir v0.3.4
