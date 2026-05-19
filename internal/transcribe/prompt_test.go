package transcribe

import (
	"strings"
	"testing"

	"github.com/julianshen/gogfy/internal/schema"
)

func TestBuildPromptFallbackWhenNoGodNodes(t *testing.T) {
	t.Setenv(promptEnvOverride, "")
	if got := BuildPrompt(nil); got != FallbackPrompt {
		t.Errorf("nil should yield fallback, got %q", got)
	}
	// Nodes with empty labels are equivalent to no nodes.
	if got := BuildPrompt([]schema.Node{{Label: ""}, {Label: "   "}}); got != FallbackPrompt {
		t.Errorf("empty-label nodes should yield fallback, got %q", got)
	}
}

func TestBuildPromptIncludesTopLabels(t *testing.T) {
	t.Setenv(promptEnvOverride, "")
	nodes := []schema.Node{
		{Label: "Kubernetes"},
		{Label: "etcd"},
		{Label: "Helm"},
	}
	got := BuildPrompt(nodes)
	for _, want := range []string{"Kubernetes", "etcd", "Helm"} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt missing %q: %s", want, got)
		}
	}
	if !strings.Contains(got, FallbackPrompt) {
		t.Errorf("prompt should still tail with punctuation instruction: %s", got)
	}
}

func TestBuildPromptCapsAtFiveLabels(t *testing.T) {
	t.Setenv(promptEnvOverride, "")
	nodes := []schema.Node{
		{Label: "A"}, {Label: "B"}, {Label: "C"}, {Label: "D"},
		{Label: "E"}, {Label: "F"}, {Label: "G"},
	}
	got := BuildPrompt(nodes)
	if !strings.Contains(got, "A, B, C, D, E.") {
		t.Errorf("expected first 5 labels in order, got %q", got)
	}
	if strings.Contains(got, "F") || strings.Contains(got, "G") {
		t.Errorf("labels past index 5 should be dropped: %s", got)
	}
}

func TestBuildPromptEnvOverrideWins(t *testing.T) {
	t.Setenv(promptEnvOverride, "speaker is a medieval historian")
	got := BuildPrompt([]schema.Node{{Label: "ignored"}})
	if got != "speaker is a medieval historian" {
		t.Errorf("env override should win, got %q", got)
	}
}

func TestBuildPromptEnvOverrideTrimmed(t *testing.T) {
	// Whitespace-only env value should be treated as unset, not as a
	// valid empty prompt that would clobber the god-node logic.
	t.Setenv(promptEnvOverride, "   \t  ")
	got := BuildPrompt([]schema.Node{{Label: "kept"}})
	if !strings.Contains(got, "kept") {
		t.Errorf("whitespace-only override should be ignored, got %q", got)
	}
}
