package fsutil

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestSHA256FileHashesContent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(p, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := SHA256File(p)
	if err != nil {
		t.Fatal(err)
	}
	// SHA-256("hello") — pinning the exact digest catches a future
	// refactor that accidentally hashes path or metadata instead of
	// content.
	want := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if got != want {
		t.Fatalf("SHA256File(hello) = %q, want %q", got, want)
	}
}

func TestSHA256FileMissingErrors(t *testing.T) {
	if _, err := SHA256File(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestSHA256FileChangesOnContentChange(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f.txt")
	_ = os.WriteFile(p, []byte("a"), 0o644)
	h1, err := SHA256File(p)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(p, []byte("b"), 0o644)
	h2, err := SHA256File(p)
	if err != nil {
		t.Fatal(err)
	}
	if h1 == h2 {
		t.Fatal("hash should change when content changes")
	}
}

func TestReadFileOrEmptyMissingReturnsNil(t *testing.T) {
	data, err := ReadFileOrEmpty(filepath.Join(t.TempDir(), "no-such-file"))
	if err != nil {
		t.Fatalf("expected nil error for ENOENT, got %v", err)
	}
	if data != nil {
		t.Fatalf("expected nil data for ENOENT, got %q", data)
	}
}

func TestReadFileOrEmptyExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	data, err := ReadFileOrEmpty(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Fatalf("got %q, want %q", data, "hello")
	}
}

func TestReadFileOrEmptyPropagatesOtherErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Skipf("chmod 0 not honored: %v", err)
	}
	defer os.Chmod(path, 0644)
	if _, err := os.ReadFile(path); err == nil {
		t.Skip("environment allows reading chmod-0 files")
	}
	if _, err := ReadFileOrEmpty(path); err == nil {
		t.Fatal("expected read error to propagate")
	}
}

func TestWriteFileAtomicCreates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "out")
	if err := WriteFileAtomic(path, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("got %q, want %q", got, "hello")
	}
	// .tmp sibling must not linger.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("expected .tmp to be cleaned up, got err=%v", err)
	}
}

func TestWriteFileAtomicReplacesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(path, []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, []byte("new")) {
		t.Fatalf("got %q, want %q", got, "new")
	}
}

func TestWriteFileAtomicRespectsPerm(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "exec")
	if err := WriteFileAtomic(path, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm()&0111 == 0 {
		t.Fatalf("expected exec bits, got %v", info.Mode())
	}
}

func TestWriteFileAtomicFailsWhenTargetIsADirectory(t *testing.T) {
	// Rename can't replace a directory with a file; this exercises the
	// rename-failure cleanup path that removes the .tmp sibling.
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(target, []byte("x"), 0644); err == nil {
		t.Fatal("expected rename error when target is a directory")
	}
	if _, err := os.Stat(target + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("expected .tmp to be cleaned up after rename failure, got err=%v", err)
	}
}

func TestWriteFileAtomicFailsWhenParentIsAFile(t *testing.T) {
	dir := t.TempDir()
	parent := filepath.Join(dir, "blocker")
	if err := os.WriteFile(parent, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	// Try to write to <blocker>/file — MkdirAll should fail.
	if err := WriteFileAtomic(filepath.Join(parent, "file"), []byte("x"), 0644); err == nil {
		t.Fatal("expected MkdirAll error when parent is a file")
	}
}
