package githook

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallMergeDriverWritesConfigAndAttributes(t *testing.T) {
	root := makeRepo(t)
	if err := InstallMergeDriver(root, MergeDriverOptions{Bin: "/opt/gogfy"}); err != nil {
		t.Fatal(err)
	}
	cfg, _ := os.ReadFile(filepath.Join(root, ".git", "config"))
	if !strings.Contains(string(cfg), `[merge "gogfy"]`) {
		t.Fatalf(".git/config missing [merge \"gogfy\"] section:\n%s", cfg)
	}
	if !strings.Contains(string(cfg), `driver = /opt/gogfy merge-graphs %A %B --out %A`) {
		t.Fatalf(".git/config missing driver line:\n%s", cfg)
	}
	attrs, _ := os.ReadFile(filepath.Join(root, ".gitattributes"))
	if !strings.Contains(string(attrs), "graphify-out/graph.json merge=gogfy") {
		t.Fatalf(".gitattributes missing rule:\n%s", attrs)
	}
}

func TestInstallMergeDriverIsIdempotent(t *testing.T) {
	root := makeRepo(t)
	if err := InstallMergeDriver(root, MergeDriverOptions{Bin: "gogfy"}); err != nil {
		t.Fatal(err)
	}
	cfgFirst, _ := os.ReadFile(filepath.Join(root, ".git", "config"))
	attrsFirst, _ := os.ReadFile(filepath.Join(root, ".gitattributes"))
	for i := 0; i < 5; i++ {
		if err := InstallMergeDriver(root, MergeDriverOptions{Bin: "gogfy"}); err != nil {
			t.Fatalf("reinstall #%d: %v", i, err)
		}
	}
	cfgN, _ := os.ReadFile(filepath.Join(root, ".git", "config"))
	attrsN, _ := os.ReadFile(filepath.Join(root, ".gitattributes"))
	if !bytes.Equal(cfgFirst, cfgN) {
		t.Fatalf(".git/config drifted across reinstalls:\nfirst:  %s\nafter5: %s", cfgFirst, cfgN)
	}
	if !bytes.Equal(attrsFirst, attrsN) {
		t.Fatalf(".gitattributes drifted across reinstalls")
	}
}

func TestInstallMergeDriverPreservesExistingConfig(t *testing.T) {
	root := makeRepo(t)
	cfgPath := filepath.Join(root, ".git", "config")
	original := []byte(`[core]
	repositoryformatversion = 0
[remote "origin"]
	url = git@example.com:foo.git
`)
	if err := os.WriteFile(cfgPath, original, 0644); err != nil {
		t.Fatal(err)
	}
	if err := InstallMergeDriver(root, MergeDriverOptions{Bin: "gogfy"}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(cfgPath)
	for _, must := range []string{
		"[core]",
		"repositoryformatversion = 0",
		`[remote "origin"]`,
		`url = git@example.com:foo.git`,
		`[merge "gogfy"]`,
	} {
		if !strings.Contains(string(got), must) {
			t.Fatalf("missing %q after install:\n%s", must, got)
		}
	}
}

func TestInstallMergeDriverPreservesExistingGitattributes(t *testing.T) {
	root := makeRepo(t)
	attrsPath := filepath.Join(root, ".gitattributes")
	original := []byte("*.go text\n*.bin binary\n")
	if err := os.WriteFile(attrsPath, original, 0644); err != nil {
		t.Fatal(err)
	}
	if err := InstallMergeDriver(root, MergeDriverOptions{Bin: "gogfy"}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(attrsPath)
	if !strings.Contains(string(got), "*.go text") || !strings.Contains(string(got), "*.bin binary") {
		t.Fatalf("pre-existing gitattributes erased:\n%s", got)
	}
	if !strings.Contains(string(got), "graphify-out/graph.json merge=gogfy") {
		t.Fatalf("merge rule not appended:\n%s", got)
	}
}

func TestUninstallMergeDriverRemovesBlocks(t *testing.T) {
	root := makeRepo(t)
	if err := InstallMergeDriver(root, MergeDriverOptions{Bin: "gogfy"}); err != nil {
		t.Fatal(err)
	}
	if err := UninstallMergeDriver(root); err != nil {
		t.Fatal(err)
	}
	cfg, _ := os.ReadFile(filepath.Join(root, ".git", "config"))
	if strings.Contains(string(cfg), `[merge "gogfy"]`) {
		t.Fatalf("merge section still in .git/config:\n%s", cfg)
	}
	// .gitattributes was created by install with only the gogfy block —
	// uninstall should leave it empty or delete it.
	if data, err := os.ReadFile(filepath.Join(root, ".gitattributes")); err == nil {
		if strings.Contains(string(data), "merge=gogfy") {
			t.Fatalf(".gitattributes still has merge=gogfy:\n%s", data)
		}
	}
}

func TestUninstallMergeDriverPreservesUnrelatedAttributes(t *testing.T) {
	root := makeRepo(t)
	attrsPath := filepath.Join(root, ".gitattributes")
	original := []byte("*.go text\n*.bin binary\n")
	if err := os.WriteFile(attrsPath, original, 0644); err != nil {
		t.Fatal(err)
	}
	if err := InstallMergeDriver(root, MergeDriverOptions{Bin: "gogfy"}); err != nil {
		t.Fatal(err)
	}
	if err := UninstallMergeDriver(root); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(attrsPath)
	if !strings.Contains(string(got), "*.go text") || !strings.Contains(string(got), "*.bin binary") {
		t.Fatalf("user rules erased on uninstall:\n%s", got)
	}
	if strings.Contains(string(got), "merge=gogfy") {
		t.Fatalf("merge rule still present:\n%s", got)
	}
}

func TestUninstallMergeDriverNoOpWhenAbsent(t *testing.T) {
	root := makeRepo(t)
	if err := UninstallMergeDriver(root); err != nil {
		t.Fatalf("expected no-op on bare repo, got %v", err)
	}
}

func TestMergeDriverInstalledReportsState(t *testing.T) {
	root := makeRepo(t)
	if MergeDriverInstalled(root) {
		t.Fatal("expected false before install")
	}
	if err := InstallMergeDriver(root, MergeDriverOptions{Bin: "gogfy"}); err != nil {
		t.Fatal(err)
	}
	if !MergeDriverInstalled(root) {
		t.Fatal("expected true after install")
	}
	if err := UninstallMergeDriver(root); err != nil {
		t.Fatal(err)
	}
	if MergeDriverInstalled(root) {
		t.Fatal("expected false after uninstall")
	}
}

func TestInstallMergeDriverFailsOnNonRepoRoot(t *testing.T) {
	root := t.TempDir() // no .git
	if err := InstallMergeDriver(root, MergeDriverOptions{}); err == nil {
		t.Fatal("expected error on non-repo root")
	}
}

func TestInstallMergeDriverRefusesMismatchedFenceMarkers(t *testing.T) {
	root := makeRepo(t)
	cfgPath := filepath.Join(root, ".git", "config")
	hostile := []byte(mergeDriverStart + "\n" + mergeDriverStart + "\nstuff\n" + mergeDriverEnd + "\n")
	if err := os.WriteFile(cfgPath, hostile, 0644); err != nil {
		t.Fatal(err)
	}
	if err := InstallMergeDriver(root, MergeDriverOptions{}); err == nil {
		t.Fatal("expected error on duplicate start markers")
	}
}

func TestUninstallMergeDriverRefusesMismatchedFence(t *testing.T) {
	root := makeRepo(t)
	cfgPath := filepath.Join(root, ".git", "config")
	hostile := []byte(mergeDriverStart + "\nfoo\n" + mergeDriverStart + "\nbar\n" + mergeDriverEnd + "\n")
	if err := os.WriteFile(cfgPath, hostile, 0644); err != nil {
		t.Fatal(err)
	}
	if err := UninstallMergeDriver(root); err == nil {
		t.Fatal("expected error on mismatched fence")
	}
}

func TestWriteFencedBlockNoOpWhenContentUnchanged(t *testing.T) {
	root := makeRepo(t)
	if err := InstallMergeDriver(root, MergeDriverOptions{Bin: "gogfy"}); err != nil {
		t.Fatal(err)
	}
	preInfo, _ := os.Stat(filepath.Join(root, ".git", "config"))
	if err := InstallMergeDriver(root, MergeDriverOptions{Bin: "gogfy"}); err != nil {
		t.Fatal(err)
	}
	postInfo, _ := os.Stat(filepath.Join(root, ".git", "config"))
	if !preInfo.ModTime().Equal(postInfo.ModTime()) {
		t.Fatalf("idempotent reinstall touched mtime")
	}
}

func TestInstallMergeDriverDefaultsBin(t *testing.T) {
	root := makeRepo(t)
	if err := InstallMergeDriver(root, MergeDriverOptions{}); err != nil {
		t.Fatal(err)
	}
	cfg, _ := os.ReadFile(filepath.Join(root, ".git", "config"))
	if !strings.Contains(string(cfg), "driver = gogfy merge-graphs") {
		t.Fatalf("default bin should be 'gogfy', got:\n%s", cfg)
	}
}
