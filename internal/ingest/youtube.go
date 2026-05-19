package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"

	"github.com/julianshen/gogfy/internal/safefetch"
)

// youTubeHostRe matches the common YouTube URL shapes — watch?v=…,
// youtu.be/…, shorts/, embed/. Detection only cares about host +
// "is-a-video"-ness; the video id parsing happens inside oembed.
var youTubeHostRe = regexp.MustCompile(`(?i)^https?://(?:(?:www\.|m\.)?youtube\.com/(?:watch\b|shorts/|embed/)|youtu\.be/)`)

// IsYouTubeURL reports whether rawURL is a YouTube video URL gogfy
// can fetch oembed metadata for. Channel / playlist URLs are
// excluded — they aren't single-video targets and oembed's behavior
// on them is inconsistent.
func IsYouTubeURL(rawURL string) bool {
	return youTubeHostRe.MatchString(rawURL)
}

// youTubeOEmbed is the subset of YouTube's oembed JSON we surface.
// Other fields (thumbnail dimensions, html embed code) are dropped —
// the goal is feeding text into the semantic pipeline, not embedding
// a player.
type youTubeOEmbed struct {
	Title        string `json:"title"`
	AuthorName   string `json:"author_name"`
	AuthorURL    string `json:"author_url"`
	ThumbnailURL string `json:"thumbnail_url"`
	ProviderName string `json:"provider_name"`
}

// fetchYouTubeMetadata retrieves oembed JSON for rawURL via
// safefetch (so the SSRF guards apply to the oembed endpoint too)
// and returns a markdown-shaped string suitable for prepending to a
// sidecar. The returned body is intentionally short — without the
// audio transcript there's not much corpus signal to add.
func fetchYouTubeMetadata(ctx context.Context, rawURL string, opts safefetch.Options) (string, error) {
	endpoint := "https://www.youtube.com/oembed?url=" +
		url.QueryEscape(rawURL) + "&format=json"
	body, _, err := safefetch.Fetch(ctx, endpoint, opts)
	if err != nil {
		return "", fmt.Errorf("youtube oembed: %w", err)
	}
	var emb youTubeOEmbed
	if err := json.Unmarshal(body, &emb); err != nil {
		return "", fmt.Errorf("youtube oembed parse: %w", err)
	}
	return formatYouTubeMetadata(emb), nil
}

// formatYouTubeMetadata renders the oembed response as a quoted
// markdown block — same style as the OpenGraph block so downstream
// consumers see one consistent "structured metadata up top, prose
// below" shape regardless of source type.
//
// Every string field is run through lineBreakRe before concatenation
// — a value with an embedded \n would otherwise break the markdown
// blockquote and let attacker-controlled (or simply unexpected)
// content escape into the sidecar's frontmatter region.
func formatYouTubeMetadata(emb youTubeOEmbed) string {
	if emb.Title == "" && emb.AuthorName == "" {
		return ""
	}
	clean := func(s string) string {
		return lineBreakRe.ReplaceAllString(s, " ")
	}
	out := "> **YouTube video**\n"
	if emb.Title != "" {
		out += "> Title: " + clean(emb.Title) + "\n"
	}
	if emb.AuthorName != "" {
		out += "> Author: " + clean(emb.AuthorName)
		if emb.AuthorURL != "" {
			out += " (" + clean(emb.AuthorURL) + ")"
		}
		out += "\n"
	}
	if emb.ThumbnailURL != "" {
		out += "> Thumbnail: " + clean(emb.ThumbnailURL) + "\n"
	}
	return out
}
