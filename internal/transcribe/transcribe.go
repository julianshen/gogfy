// Package transcribe defines the audio/video → text contract gogfy
// uses to turn media files into prose that can flow through the
// existing semantic-extraction pipeline. The interface is intentionally
// minimal — a Transcriber takes raw audio bytes plus a MIME hint and
// returns plain text — so future backends (OpenAI Whisper API,
// whisper.cpp via CGo, self-hosted OpenAI-compatible endpoints) can
// satisfy it without touching call sites.
package transcribe

import "context"

// VideoExtensions / AudioExtensions are the file extensions gogfy
// treats as transcribable. Kept exported so detect/extract layers can
// route based on the same list the backends will accept.
//
// Mirrors upstream graphify's VIDEO_EXTENSIONS set; the audio/video
// split is for reporting only — both go through the same API.
var (
	VideoExtensions = map[string]bool{
		".mp4": true, ".mov": true, ".webm": true, ".mkv": true,
		".avi": true, ".m4v": true,
	}
	AudioExtensions = map[string]bool{
		".mp3": true, ".wav": true, ".m4a": true, ".ogg": true,
		".flac": true,
	}
)

// IsTranscribable reports whether ext (with leading dot, lowercase)
// names a media file the pipeline should route through a Transcriber.
func IsTranscribable(ext string) bool {
	return VideoExtensions[ext] || AudioExtensions[ext]
}

// Request bundles the inputs every backend needs. Filename carries the
// extension hint OpenAI's multipart form requires; MIME is a stronger
// signal for backends that respect it.
type Request struct {
	Filename string // logical name with extension, e.g. "talk.mp4"
	MimeType string // e.g. "audio/mpeg" — empty is acceptable
	Audio    []byte
	// Prompt biases the model's output — useful for terminology the
	// model wouldn't otherwise know. Upstream uses god-node labels.
	Prompt string
	// Language as an ISO-639-1 code (e.g. "en"). Empty = auto-detect.
	Language string
}

// Response is what every backend returns. Text is the only required
// field; duration/cost are best-effort metadata for the cost report.
type Response struct {
	Text             string
	DurationSeconds  float64
	EstimatedUSDCost float64
}

// Client is the contract every transcription backend implements. One
// verb — same shape as llm.Client.Generate — so swapping backends is a
// one-line registry change.
type Client interface {
	Transcribe(ctx context.Context, req Request) (Response, error)
	Name() string
}
