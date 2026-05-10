package extract

import "testing"

func TestTrimQuotes(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{`"hello"`, "hello"},
		{`'hello'`, "hello"},
		{"`hello`", "hello"},
		{`"foo'`, "foo"},          // mixed delimiters; strings.Trim treats the cutset as a set
		{"hello", "hello"},        // bare string — idempotent
		{"", ""},                  // empty
		{`"`, ""},                 // single quote — collapses to empty
		{`""`, ""},                // matched empty quotes
		{"<foo>", "<foo>"},        // angle brackets are NOT trimmed (TS generics, paths)
		{`"a"b"c"`, "a\"b\"c"},    // only outer quotes stripped
		{`"path/with/slashes"`, "path/with/slashes"},
	}
	for _, c := range cases {
		got := trimQuotes(c.in)
		if got != c.want {
			t.Errorf("trimQuotes(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
