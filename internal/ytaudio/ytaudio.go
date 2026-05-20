// Package ytaudio downloads the audio stream of a YouTube video as a
// raw byte slice. Wraps github.com/kkdai/youtube/v2 — a pure-Go
// YouTube client that handles the parts of yt-dlp's job that aren't
// easily reproducible (player-JS extraction, signature deciphering,
// itag selection). gogfy's no-shell-out policy rules out yt-dlp
// itself; a pure-Go dep is the closest alternative.
//
// The returned bytes are the raw stream (typically m4a or webm/opus
// depending on what YouTube serves). Downstream callers — currently
// the transcribe pipeline — feed those bytes straight into Whisper /
// the configured transcribe backend with a matching MIME hint.
package ytaudio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"

	yt "github.com/kkdai/youtube/v2"
)

// Result is what Download returns: the audio bytes, a content-type
// suitable for the transcribe pipeline's multipart form, the video's
// duration, and the title (the existing oembed path also surfaces a
// title; both are kept available so callers don't have to make two
// requests).
type Result struct {
	Audio       []byte
	ContentType string
	Duration    time.Duration
	Title       string
	Author      string
}

// ErrNoAudioStream is returned when YouTube's player response lists
// no audio-only or audio-bearing streams — happens for videos that
// are upload-in-progress, region-locked, or have been taken down.
var ErrNoAudioStream = errors.New("ytaudio: no audio stream available")

// Client wraps a yt.Client so tests can swap the underlying HTTP
// transport and so callers can share one HTTP client across many
// downloads (reuses the TCP pool).
type Client struct {
	yt   *yt.Client
	http *http.Client
}

// Option tweaks a Client. Functional-options keep the API stable as
// we add knobs (proxy, max-bitrate, timeout overrides).
type Option func(*Client)

// WithHTTPClient swaps the HTTP transport — primarily useful for
// tests (httptest.NewServer) and for users behind corporate proxies.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// New returns a Client with a 5-minute HTTP timeout. Matches the
// transcribe client's ceiling so a slow YouTube response doesn't
// outrun the downstream Whisper call's budget.
func New(opts ...Option) *Client {
	c := &Client{
		http: &http.Client{Timeout: 5 * time.Minute},
	}
	for _, o := range opts {
		o(c)
	}
	c.yt = &yt.Client{HTTPClient: c.http}
	return c
}

// IngestFetcher wraps a Client so it satisfies the structural
// interface `internal/ingest.YouTubeAudioFetcher` (which uses a
// flattened return signature to avoid a `ytaudio.Result` import in
// the ingest package). Construct with NewIngestFetcher.
type IngestFetcher struct{ c *Client }

// NewIngestFetcher returns an IngestFetcher that delegates to c.
func NewIngestFetcher(c *Client) *IngestFetcher { return &IngestFetcher{c: c} }

// Download flattens the Result into the (audio, contentType, title,
// author, err) tuple shape the ingest interface expects.
func (f *IngestFetcher) Download(ctx context.Context, videoURL string) (audio []byte, contentType, title, author string, err error) {
	r, e := f.c.Download(ctx, videoURL)
	if e != nil {
		return nil, "", "", "", e
	}
	return r.Audio, r.ContentType, r.Title, r.Author, nil
}

// Download fetches the audio stream of the YouTube video at videoURL.
// Picks the audio-only stream with the highest bitrate when present;
// falls back to the smallest audio-bearing combined stream otherwise.
// Returns ErrNoAudioStream if neither is available.
func (c *Client) Download(ctx context.Context, videoURL string) (Result, error) {
	video, err := c.yt.GetVideoContext(ctx, videoURL)
	if err != nil {
		return Result{}, fmt.Errorf("ytaudio: get video: %w", err)
	}
	format := pickAudioFormat(video.Formats)
	if format == nil {
		return Result{}, ErrNoAudioStream
	}
	stream, _, err := c.yt.GetStreamContext(ctx, video, format)
	if err != nil {
		return Result{}, fmt.Errorf("ytaudio: get stream: %w", err)
	}
	defer stream.Close()
	body, err := io.ReadAll(stream)
	if err != nil {
		return Result{}, fmt.Errorf("ytaudio: read stream: %w", err)
	}
	return Result{
		Audio:       body,
		ContentType: format.MimeType,
		Duration:    video.Duration,
		Title:       video.Title,
		Author:      video.Author,
	}, nil
}

// pickAudioFormat ranks YouTube's offered formats for transcription
// suitability. The priorities, in order:
//   1. Audio-only streams (lower bandwidth, simpler decode) sorted
//      descending by bitrate so we get the cleanest audio Whisper
//      can ingest without paying for video chrome.
//   2. If no audio-only stream is offered (live streams, some shorts),
//      fall back to the smallest video+audio combined stream — at
//      that point we just need *some* audio track for the transcriber.
//
// Returns nil iff Formats has no streams with audio at all.
func pickAudioFormat(formats yt.FormatList) *yt.Format {
	var audioOnly []yt.Format
	var combined []yt.Format
	for _, f := range formats {
		if f.AudioChannels == 0 {
			continue
		}
		if f.FPS == 0 { // FPS=0 ⇒ no video track ⇒ audio-only
			audioOnly = append(audioOnly, f)
		} else {
			combined = append(combined, f)
		}
	}
	if len(audioOnly) > 0 {
		sort.Slice(audioOnly, func(i, j int) bool {
			return audioOnly[i].Bitrate > audioOnly[j].Bitrate
		})
		return &audioOnly[0]
	}
	if len(combined) > 0 {
		sort.Slice(combined, func(i, j int) bool {
			return combined[i].ContentLength < combined[j].ContentLength
		})
		return &combined[0]
	}
	return nil
}
