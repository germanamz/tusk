package linenum_test

import (
	"testing"

	"github.com/germanamz/tusk/internal/linenum"
)

func TestParseScheme(test *testing.T) {
	cases := []struct {
		input   string
		want    linenum.Scheme
		wantErr bool
	}{
		{input: "", want: linenum.SchemeLF},
		{input: "lf", want: linenum.SchemeLF},
		{input: "universal", want: linenum.SchemeUniversal},
		{input: "unicode", want: linenum.SchemeUnicode},
		{input: "crlf", wantErr: true},
		{input: "LF", wantErr: true},
	}

	for _, testCase := range cases {
		got, parseErr := linenum.ParseScheme(testCase.input)

		if testCase.wantErr {
			if parseErr == nil {
				test.Errorf("ParseScheme(%q) = %q, want an error", testCase.input, got)
			}

			continue
		}

		if parseErr != nil {
			test.Errorf("ParseScheme(%q) error: %v", testCase.input, parseErr)

			continue
		}

		if got != testCase.want {
			test.Errorf("ParseScheme(%q) = %q, want %q", testCase.input, got, testCase.want)
		}
	}
}

// TestTable_Line pins which byte sequences end a line under each scheme. The
// probe is the byte offset of the marker letter, so the expected value is the
// 1-based line that letter sits on.
func TestTable_Line(test *testing.T) {
	cases := []struct {
		name   string
		source string
		marker byte
		want   map[linenum.Scheme]int
	}{
		{
			name:   "lf",
			source: "a\nb\nX",
			marker: 'X',
			want:   map[linenum.Scheme]int{linenum.SchemeLF: 3, linenum.SchemeUniversal: 3, linenum.SchemeUnicode: 3},
		},
		{
			name:   "crlf counts once",
			source: "a\r\nb\r\nX",
			marker: 'X',
			want:   map[linenum.Scheme]int{linenum.SchemeLF: 3, linenum.SchemeUniversal: 3, linenum.SchemeUnicode: 3},
		},
		{
			name:   "lone cr",
			source: "a\rb\nX",
			marker: 'X',
			want:   map[linenum.Scheme]int{linenum.SchemeLF: 2, linenum.SchemeUniversal: 3, linenum.SchemeUnicode: 3},
		},
		{
			name:   "unicode separators",
			source: "a b c\u0085d\ve\fX",
			marker: 'X',
			want:   map[linenum.Scheme]int{linenum.SchemeLF: 1, linenum.SchemeUniversal: 1, linenum.SchemeUnicode: 6},
		},
		{
			name:   "first byte",
			source: "X\nb",
			marker: 'X',
			want:   map[linenum.Scheme]int{linenum.SchemeLF: 1, linenum.SchemeUniversal: 1, linenum.SchemeUnicode: 1},
		},
	}

	for _, testCase := range cases {
		offset := indexByte(testCase.source, testCase.marker)

		for scheme, want := range testCase.want {
			table := linenum.NewTable([]byte(testCase.source), scheme)

			if got := table.Line(offset); got != want {
				test.Errorf("%s: %s Line(%d) = %d, want %d", testCase.name, scheme, offset, got, want)
			}
		}
	}
}

// TestTable_TerminatorBelongsToItsLine: the bytes of a terminator sit on the
// line they end, so the last content byte's line is stable whether a range end
// lands on the content or on its trailing newline.
func TestTable_TerminatorBelongsToItsLine(test *testing.T) {
	table := linenum.NewTable([]byte("ab\r\ncd"), linenum.SchemeUniversal)

	for offset, want := range []int{1, 1, 1, 1, 2, 2} {
		if got := table.Line(offset); got != want {
			test.Errorf("Line(%d) = %d, want %d", offset, got, want)
		}
	}
}

func indexByte(source string, marker byte) int {
	for index := range len(source) {
		if source[index] == marker {
			return index
		}
	}

	return -1
}
