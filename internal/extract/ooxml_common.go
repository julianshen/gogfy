package extract

import (
	"encoding/xml"
	"path"
	"strings"
)

// OOXML relationship-type discriminators. Full URLs are
// "http://schemas.openxmlformats.org/officeDocument/2006/relationships/<kind>"
// — we only care about the trailing suffix for filtering.
const (
	relTypeHyperlink = "/hyperlink"
	relTypeWorksheet = "/worksheet"
	relTypeSlide     = "/slide"
	relTypeTable     = "/table"
)

// resolveOOXMLPartPath turns a Target attribute from an OOXML _rels file
// into a zip-entry path. Targets come in two forms: relative to the part's
// directory (`slides/slide1.xml`, `worksheets/sheet1.xml` — what PowerPoint
// and Excel emit) and package-absolute (`/ppt/slides/slide1.xml`,
// `/xl/worksheets/sheet1.xml` — what the Open XML SDK emits). Treating an
// absolute path as relative produced `ppt/ppt/slides/...` and missed every
// part; this helper handles both forms uniformly.
func resolveOOXMLPartPath(partRoot, target string) string {
	if rest, ok := strings.CutPrefix(target, "/"); ok {
		return rest
	}
	// Target may use parent-dir references (../tables/table1.xml from a
	// worksheet's _rels file). path.Clean normalizes them so the result
	// matches the actual zip entry name.
	return path.Clean(partRoot + target)
}

// parseOOXMLRels parses an OOXML _rels file into Id→Target. If wantSuffix
// is non-empty, only relationships whose Type ends with it are returned.
// For hyperlink rels we additionally require TargetMode="External": OOXML
// hyperlinks can target bookmarks or internal parts, and emitting those
// as cross-document references would pollute the graph the same way the
// body walker's w:anchor skip prevents.
func parseOOXMLRels(data []byte, wantSuffix string) map[string]string {
	out := map[string]string{}
	if len(data) == 0 {
		return out
	}
	type relationship struct {
		ID         string `xml:"Id,attr"`
		Type       string `xml:"Type,attr"`
		Target     string `xml:"Target,attr"`
		TargetMode string `xml:"TargetMode,attr"`
	}
	type relationships struct {
		Items []relationship `xml:"Relationship"`
	}
	var rs relationships
	if err := xml.Unmarshal(data, &rs); err != nil {
		return out
	}
	for _, r := range rs.Items {
		if wantSuffix != "" && !strings.HasSuffix(r.Type, wantSuffix) {
			continue
		}
		if wantSuffix == relTypeHyperlink && r.TargetMode != "External" {
			continue
		}
		out[r.ID] = r.Target
	}
	return out
}
