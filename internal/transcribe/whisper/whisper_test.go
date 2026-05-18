package whisper

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/julianshen/gogfy/internal/transcribe"
)

func TestNewRequiresAPIKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	_, err := New()
	if !errors.Is(err, ErrMissingAPIKey) {
		t.Fatalf("expected ErrMissingAPIKey, got %v", err)
	}
}

func TestTranscribeEmptyAudioRejected(t *testing.T) {
	c := NewWithKey("sk-test")
	_, err := c.Transcribe(context.Background(), transcribe.Request{Filename: "x.mp3"})
	if err == nil || !strings.Contains(err.Error(), "empty audio") {
		t.Errorf("expected empty-audio error, got %v", err)
	}
}

func TestTranscribeRoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			t.Errorf("missing Bearer auth: %v", r.Header)
		}
		ct, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || ct != "multipart/form-data" {
			t.Fatalf("expected multipart/form-data, got %q", r.Header.Get("Content-Type"))
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		gotFields := map[string]string{}
		gotFile := false
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if p.FileName() != "" {
				gotFile = true
				if p.FileName() != "talk.mp3" {
					t.Errorf("filename not propagated: %q", p.FileName())
				}
				continue
			}
			b, _ := io.ReadAll(p)
			gotFields[p.FormName()] = string(b)
		}
		if !gotFile {
			t.Error("multipart form missing file part")
		}
		if gotFields["model"] != DefaultModel {
			t.Errorf("model field: %q", gotFields["model"])
		}
		if gotFields["response_format"] != "verbose_json" {
			t.Errorf("response_format should be verbose_json (needed for duration): %q", gotFields["response_format"])
		}
		if gotFields["prompt"] != "topic hints" {
			t.Errorf("prompt not propagated: %q", gotFields["prompt"])
		}
		if gotFields["language"] != "en" {
			t.Errorf("language not propagated: %q", gotFields["language"])
		}
		_ = json.NewEncoder(w).Encode(transcriptionResponse{
			Text:     "hello world",
			Duration: 120, // 2 minutes
		})
	}))
	defer srv.Close()

	c := NewWithKey("sk-test", WithEndpoint(srv.URL))
	got, err := c.Transcribe(context.Background(), transcribe.Request{
		Filename: "talk.mp3",
		Audio:    []byte("fake mp3 bytes"),
		Prompt:   "topic hints",
		Language: "en",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "hello world" {
		t.Errorf("text not relayed: %q", got.Text)
	}
	if got.DurationSeconds != 120 {
		t.Errorf("duration not relayed: %v", got.DurationSeconds)
	}
	// 2 minutes * $0.006/min = $0.012
	wantCost := 2 * 0.006
	if got.EstimatedUSDCost < wantCost-1e-9 || got.EstimatedUSDCost > wantCost+1e-9 {
		t.Errorf("cost wrong: got %v want %v", got.EstimatedUSDCost, wantCost)
	}
}

func TestTranscribeOmitsOptionalFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			// prompt and language are optional — they must not be sent
			// as empty-string fields, since some compatible servers
			// reject empty language codes outright.
			if name := p.FormName(); name == "prompt" || name == "language" {
				t.Errorf("optional field %q should be omitted when empty", name)
			}
		}
		_ = json.NewEncoder(w).Encode(transcriptionResponse{Text: "x", Duration: 1})
	}))
	defer srv.Close()
	c := NewWithKey("k", WithEndpoint(srv.URL))
	_, err := c.Transcribe(context.Background(), transcribe.Request{Filename: "x.mp3", Audio: []byte("a")})
	if err != nil {
		t.Fatal(err)
	}
}

func TestTranscribeSurfacesAPIErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(413)
		_, _ = io.WriteString(w, `{"error":{"message":"file too large","type":"invalid_request_error"}}`)
	}))
	defer srv.Close()
	c := NewWithKey("sk-test", WithEndpoint(srv.URL))
	_, err := c.Transcribe(context.Background(), transcribe.Request{Filename: "x.mp3", Audio: []byte("a")})
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Errorf("error body should surface: %v", err)
	}
}

func TestNameIncludesModelTag(t *testing.T) {
	c := NewWithKey("k", WithModel("whisper-large-v3"))
	if c.Name() != "whisper-whisper-large-v3" {
		t.Errorf("Name() = %q", c.Name())
	}
}

func TestBuildMultipartUsesFallbackFilename(t *testing.T) {
	r, _, err := buildMultipart(transcribe.Request{Audio: []byte("a")}, "m")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(r)
	// Must contain a filename with .mp3 extension — OpenAI rejects
	// uploads where the multipart filename has no extension.
	if !strings.Contains(string(raw), `filename="audio.mp3"`) {
		t.Errorf("expected fallback filename audio.mp3 in body: %s", raw)
	}
}
