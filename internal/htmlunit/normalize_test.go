package htmlunit

import "testing"

func TestNormalizeText_BlocksAndInline(test *testing.T) {
	src := []byte(`<head><title>T</title></head>` +
		`<p>First   <em>para</em> &amp; more</p>` +
		`<p>Second para</p>` +
		`<script>ignore()</script>` +
		`<style>.x{}</style>`)
	got := NormalizeText(src)
	want := "First para & more\n\nSecond para"
	if got != want {
		test.Fatalf("normalize:\n want %q\n  got %q", want, got)
	}
}

func TestNormalizeText_Deterministic(test *testing.T) {
	src := []byte(`<h1>Heading</h1><p>Body</p><ul><li>One</li><li>Two</li></ul>`)
	// Two separate variables (rather than NormalizeText(src) on both
	// sides of !=) so staticcheck SA4000 does not flag the comparison
	// as trivially identical; the intent is determinism.
	first := NormalizeText(src)
	second := NormalizeText(src)
	if first != second {
		test.Fatalf("NormalizeText is not deterministic: %q vs %q", first, second)
	}
}

func TestNormalizeText_Empty(test *testing.T) {
	if got := NormalizeText([]byte(`<head><title>only head</title></head>`)); got != "" {
		test.Fatalf("want empty body, got %q", got)
	}
}
