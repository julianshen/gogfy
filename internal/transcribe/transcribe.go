// Package transcribe defines the audio/video → text contract gogfy
// uses to turn media files into prose that can flow through the
// existing semantic-extraction pipeline. The interface is intentionally
// minimal — a Transcriber takes raw audio bytes plus a MIME hint and
// returns plain text — so future backends (OpenAI Whisper API,
// whisper.cpp via CGo, self-hosted OpenAI-compatible endpoints) can
// satisfy it without touching call sites.
package transcribe

import "context"

// VideoExtensions and AudioExtensions mirror upstream graphify's media
// sets. The split is for reporting only — both route through the same
// Transcriber API.
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
// extension hint OpenAI's multipart form requires; MimeType is a
// stronger signal for backends that respect it.
type Request struct {
	Filename string
	MimeType string
	Audio    []byte
	// Prompt biases the model's output toward known terminology;
	// upstream graphify seeds this with god-node labels.
	Prompt string
	// Language as an ISO-639-1 code (e.g. "en"). Empty = auto-detect.
	Language string
}

// Response carries the transcribed text plus best-effort metadata used
// by the cost report.
type Response struct {
	Text             string
	DurationSeconds  float64
	EstimatedUSDCost float64
}

// Client is the contract every transcription backend implements.
type Client interface {
	Transcribe(ctx context.Context, req Request) (Response, error)
	Name() string
}
