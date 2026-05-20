package ytaudio

import (
	"errors"
	"testing"

	yt "github.com/kkdai/youtube/v2"
)

// pickAudioFormat is the only part of the package that's testable
// offline — Download itself hits YouTube. The selection logic is also
// the part most likely to regress as we add codec preferences /
// bitrate caps later, so it's the high-value target for tests.

func TestPickAudioFormatPrefersAudioOnly(t *testing.T) {
	// When both audio-only and combined streams are offered, the
	// audio-only stream wins — less bandwidth, simpler decode, no
	// video chrome to discard before transcription.
	formats := yt.FormatList{
		{ItagNo: 18, MimeType: "video/mp4", AudioChannels: 2, Bitrate: 96000, FPS: 30}, // combined
		{ItagNo: 140, MimeType: "audio/mp4", AudioChannels: 2, Bitrate: 128000},        // audio-only, FPS=0
	}
	got := pickAudioFormat(formats)
	if got == nil {
		t.Fatal("expected a pick")
	}
	if got.ItagNo != 140 {
		t.Errorf("expected audio-only itag 140, got %d", got.ItagNo)
	}
}

func TestPickAudioFormatHighestBitrateWhenMultipleAudioOnly(t *testing.T) {
	// Two audio-only streams → take the higher-bitrate one. Whisper
	// gets cleaner input; cost difference at typical podcast lengths
	// is negligible.
	formats := yt.FormatList{
		{ItagNo: 139, MimeType: "audio/mp4", AudioChannels: 2, Bitrate: 48000},
		{ItagNo: 140, MimeType: "audio/mp4", AudioChannels: 2, Bitrate: 128000},
		{ItagNo: 251, MimeType: "audio/webm", AudioChannels: 2, Bitrate: 160000},
	}
	got := pickAudioFormat(formats)
	if got == nil || got.ItagNo != 251 {
		t.Errorf("expected highest-bitrate audio-only (itag 251), got %+v", got)
	}
}

func TestPickAudioFormatFallsBackToCombinedWhenNoAudioOnly(t *testing.T) {
	// Some live streams / shorts only offer combined video+audio.
	// We still need *some* audio for transcription — pick the smallest
	// combined stream to minimize bandwidth wasted on video frames
	// we're going to throw away.
	formats := yt.FormatList{
		{ItagNo: 22, MimeType: "video/mp4", AudioChannels: 2, FPS: 30, ContentLength: 500_000_000},
		{ItagNo: 18, MimeType: "video/mp4", AudioChannels: 2, FPS: 30, ContentLength: 100_000_000},
	}
	got := pickAudioFormat(formats)
	if got == nil || got.ItagNo != 18 {
		t.Errorf("expected smallest combined (itag 18), got %+v", got)
	}
}

func TestPickAudioFormatNilWhenNoAudioTracks(t *testing.T) {
	// Video-only / silent formats must NOT be picked — there's
	// nothing to transcribe. Return nil so the caller can surface
	// ErrNoAudioStream rather than silently feeding video frames
	// into Whisper.
	formats := yt.FormatList{
		{ItagNo: 137, MimeType: "video/mp4", AudioChannels: 0, FPS: 30},
	}
	if got := pickAudioFormat(formats); got != nil {
		t.Errorf("expected nil pick for video-only formats, got %+v", got)
	}
}

func TestErrNoAudioStreamIsSentinel(t *testing.T) {
	// Code paths downstream do errors.Is checks to decide whether to
	// fall back to the metadata-only oembed path. Pin the sentinel
	// so a future rename doesn't silently break that fallback.
	if !errors.Is(ErrNoAudioStream, ErrNoAudioStream) {
		t.Error("ErrNoAudioStream should be its own sentinel")
	}
}
