package detect

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsSensitive(t *testing.T) {
	sensitive := []string{
		".env",
		".env.production",
		"config/.env.local",
		".envrc",
		"server.pem",
		"client.KEY",
		"cert.crt",
		"secrets.yaml",
		"password.txt",
		"my-token-file.json",
		"id_rsa",
		"id_ed25519.pub",
		".netrc",
		".pgpass",
		".htpasswd",
		"aws_credentials",
		"gcloud_credentials.json",
		"service.account.json",
	}
	for _, p := range sensitive {
		if !IsSensitive(p) {
			t.Errorf("expected sensitive: %q", p)
		}
	}

	benign := []string{
		"main.go",
		"README.md",
		"src/auth.go",       // word "auth" alone shouldn't match — "credential" is too specific
		"docs/encryption.md", // "encryption" doesn't match the credential pattern
		".gitignore",
		"server.go",
		"key_test.go", // tests for code that handles keys aren't themselves secrets
	}
	for _, p := range benign {
		if IsSensitive(p) {
			t.Errorf("unexpected sensitive flag on benign path: %q", p)
		}
	}
}

func TestIsSensitiveWindowsBackslashPaths(t *testing.T) {
	// Detection must work regardless of separator so Windows callers
	// don't end up with a permissive corpus walk.
	if !IsSensitive(`C:\Users\me\.env`) {
		t.Errorf("backslash-separated .env should match")
	}
	if !IsSensitive(`secrets\private_key.pem`) {
		t.Errorf("backslash-separated .pem should match")
	}
}

func TestCollectFilesSkipsSensitive(t *testing.T) {
	t.Helper()
	// .env should never make it into the corpus even if its extension
	// would otherwise match. Pinned at the CollectFiles boundary so an
	// extension allowlist that includes .py doesn't silently sweep up
	// a .env.py (yes, those exist) — actually .env.py has ext .py
	// which is allowed by the extension filter; sensitivity must
	// override.
	dir := t.TempDir()
	mustWrite(t, dir+"/main.py", "print('ok')\n")
	mustWrite(t, dir+"/.env", "SECRET=hunter2\n")
	mustWrite(t, dir+"/server.pem", "-----BEGIN CERTIFICATE-----\n")
	mustWrite(t, dir+"/aws_credentials.txt", "AKIA...\n")
	got, err := CollectFiles(dir, []string{".py", ".pem", ".txt", ""})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range got {
		if IsSensitive(p) {
			t.Errorf("CollectFiles must skip sensitive path: %s", p)
		}
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestIsSensitiveUnderscoreVsHyphenWordBoundary(t *testing.T) {
	// Go regexp's `\b` boundary treats `_` as a word character, so
	// underscore-separated source filenames stay outside the credential
	// pattern. Hyphen and dot ARE word boundaries, so hyphen-separated
	// names hit the pattern. Pin both so a future maintainer doesn't
	// "tighten" the regex without realizing they're flipping common
	// idiomatic Go/Python filenames.
	underscoreSafe := []string{
		"token_handler.go",
		"password_strength.go",
		"secret_test.go",
	}
	for _, p := range underscoreSafe {
		if IsSensitive(p) {
			t.Errorf("underscore-separated %q must NOT match (\\b doesn't break inside \\w)", p)
		}
	}
	hyphenAndDotMatch := []string{
		"token-handler.json",
		"password-strength.txt",
	}
	for _, p := range hyphenAndDotMatch {
		if !IsSensitive(p) {
			t.Errorf("hyphen-separated %q SHOULD match (\\b fires on `-`)", p)
		}
	}
}

func TestCollectFilesSkipsSensitiveInSubdirectory(t *testing.T) {
	// Sensitive files in nested dirs must also be skipped without
	// blocking descent into benign sibling subtrees.
	root := t.TempDir()
	mustWrite(t, root+"/keep.py", "")
	if err := os.MkdirAll(root+"/config", 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, root+"/config/.env", "")
	mustWrite(t, root+"/config/app.py", "")
	if err := os.MkdirAll(root+"/lib", 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, root+"/lib/secrets.yaml", "")
	mustWrite(t, root+"/lib/server.pem", "")

	got, err := CollectFiles(root, []string{".py", ".yaml", ".pem", ""})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range got {
		if IsSensitive(p) {
			t.Errorf("nested sensitive path leaked: %s", p)
		}
	}
	// Benign nested file must survive — the directory walk must not bail
	// out just because a sibling was sensitive.
	hasBenignNested := false
	for _, p := range got {
		if filepath.Base(p) == "app.py" {
			hasBenignNested = true
		}
	}
	if !hasBenignNested {
		t.Fatalf("benign nested file missing — walk shouldn't bail on sensitive siblings: got %v", got)
	}
}

func TestIsSensitiveExpandedCloudAndInfraPaths(t *testing.T) {
	// Path-anchored cloud / infra credential stores. The basenames are
	// generic (`config`, `credentials`) — anchoring on the parent
	// directory keeps everyday `config.go` / `credentials.go` source
	// out of the match set.
	sensitive := []string{
		".aws/credentials",
		"home/me/.aws/config",
		".kube/config",
		"deeply/nested/.kube/config",
		".docker/config.json",
		".gnupg/private-keys-v1.d/abc.key",
		".npmrc",
		".pypirc",
		".gem/credentials",
		".cargo/credentials.toml",
		"infra/main.tfstate",
		"main.tfstate.backup",
		"vars.tfvars",
		"prod.tfvars.json",
		"gogfy-firebase-adminsdk-12345.json",
		"apikey.txt",
		"access-token.env",
		"client_secret.json",
	}
	for _, p := range sensitive {
		if !IsSensitive(p) {
			t.Errorf("expected sensitive: %q", p)
		}
	}

	// Negative cases: source files with similar-looking names should
	// stay in the corpus. (Note: `internal/credentials.go` would still
	// match the pre-existing `credential|...` pattern by basename — a
	// known upstream-graphify trade-off — so it's not listed here.)
	benign := []string{
		"cmd/config.go",
		"src/kube.go",
		"docs/terraform-guide.md",
		"app/firebase.go", // no "-adminsdk-" segment
		"config.yaml",     // bare; only path-anchored .docker/config.json / .kube/config / .aws/config match
	}
	for _, p := range benign {
		if IsSensitive(p) {
			t.Errorf("unexpected sensitive flag on benign path: %q", p)
		}
	}
}
