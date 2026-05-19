package transcribe

import (
	"os"
	"strings"

	"github.com/julianshen/gogfy/internal/schema"
)

// FallbackPrompt is used when no god-node labels are available. Tells
// Whisper to punctuate normally — without this the default output
// runs on as a single sentence with no breaks.
const FallbackPrompt = "Use proper punctuation and paragraph breaks."

// promptEnvOverride lets users (or coding agents like Claude Code)
// inject a hand-crafted prompt without touching the pipeline. Same
// env name graphify uses so workflows that already export it work
// against gogfy unchanged.
const promptEnvOverride = "GOGFY_WHISPER_PROMPT"

// BuildPrompt returns a Whisper `prompt` string biased by the
// corpus's god nodes. Top labels are folded into a topic sentence so
// the model has corpus-specific vocabulary to anchor against — names,
// acronyms, and uncommon technical terms it might otherwise mis-hear.
//
// Behavior matches upstream graphify's build_whisper_prompt: at most
// 5 labels, env override wins, fallback when no labels are usable.
func BuildPrompt(godNodes []schema.Node) string {
	if override := strings.TrimSpace(os.Getenv(promptEnvOverride)); override != "" {
		return override
	}
	labels := make([]string, 0, 5)
	for _, n := range godNodes {
		l := strings.TrimSpace(n.Label)
		if l == "" {
			continue
		}
		labels = append(labels, l)
		if len(labels) == 5 {
			break
		}
	}
	if len(labels) == 0 {
		return FallbackPrompt
	}
	return "Technical discussion about " + strings.Join(labels, ", ") +
		". " + FallbackPrompt
}
