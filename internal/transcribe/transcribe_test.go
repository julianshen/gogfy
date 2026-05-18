package transcribe

import "testing"

func TestIsTranscribable(t *testing.T) {
	cases := []struct {
		ext  string
		want bool
	}{
		{".mp4", true},
		{".mov", true},
		{".mp3", true},
		{".wav", true},
		{".ogg", true},
		{".flac", true},
		{".txt", false},
		{".go", false},
		{".pdf", false},
		{"", false},
		// Case sensitivity is the caller's responsibility — pipeline
		// normalizes to lowercase before consulting this map.
		{".MP4", false},
	}
	for _, tc := range cases {
		if got := IsTranscribable(tc.ext); got != tc.want {
			t.Errorf("IsTranscribable(%q) = %v, want %v", tc.ext, got, tc.want)
		}
	}
}

func TestVideoAndAudioSetsDisjoint(t *testing.T) {
	// A given extension must classify as exactly one of video or
	// audio — overlap would make the reporting split ambiguous.
	for ext := range VideoExtensions {
		if AudioExtensions[ext] {
			t.Errorf("%q appears in both VideoExtensions and AudioExtensions", ext)
		}
	}
}
