package detect

import (
	"os"
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
