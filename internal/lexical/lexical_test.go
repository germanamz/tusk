package lexical_test

import (
	"math"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/lexical"
)

func TestTokens(test *testing.T) {
	cases := []struct {
		name string
		text string
		want []string
	}{
		{
			name: "lowercases and splits on punctuation",
			text: "Hub.Broadcast([]byte) fans OUT",
			want: []string{"hub", "broadcast", "byt", "fan", "out"},
		},
		{
			name: "drops stopwords",
			text: "what happens to the reader when it is slow",
			want: []string{"happen", "read", "slow"},
		},
		{
			name: "strips common suffixes",
			text: "dropping dropped drops slowly readers",
			want: []string{"dropp", "dropp", "drop", "slow", "read"},
		},
		{
			name: "keeps a stem of at least three bytes",
			text: "uses bus red",
			want: []string{"use", "bus", "red"},
		},
		{
			name: "drops a final e so a word meets its plural",
			text: "node nodes consume consumes consumed types",
			want: []string{"nod", "nod", "consum", "consum", "consum", "typ"},
		},
		{
			name: "keeps digits and non-ASCII letters",
			text: "num-ctx 2048 café",
			want: []string{"num", "ctx", "2048", "café"},
		},
		{
			name: "empty",
			text: "  ... ",
			want: nil,
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			got := lexical.Tokens(testCase.text)

			if !slices.Equal(got, testCase.want) {
				test.Errorf("Tokens(%q) = %q, want %q", testCase.text, got, testCase.want)
			}
		})
	}
}

func TestScores_BestMatchScoresOne(test *testing.T) {
	docs := []string{
		"The hub polls the change source and broadcasts a payload.",
		"Broadcast drops the frame for a client whose channel is full rather than blocking on a slow reader.",
		"The guard answers 403 to a request whose Host header doesn't match.",
	}

	scores := lexical.Scores("slow reader dropped frame", docs)

	if len(scores) != len(docs) {
		test.Fatalf("len(scores) = %d, want %d", len(scores), len(docs))
	}

	if scores[1] != 1 {
		test.Errorf("best match scored %v, want 1", scores[1])
	}

	if scores[0] != 0 || scores[2] != 0 {
		test.Errorf("docs sharing no query term scored %v and %v, want 0", scores[0], scores[2])
	}
}

func TestScores_RarerTermWeighsMore(test *testing.T) {
	docs := []string{
		"index rebuild",
		"index swap",
		"index epoch",
		"index sentinel",
	}

	scores := lexical.Scores("index sentinel", docs)

	if scores[3] != 1 {
		test.Fatalf("doc with the rare term scored %v, want 1", scores[3])
	}

	for position := range 3 {
		if scores[position] <= 0 || scores[position] >= 1 {
			test.Errorf("doc %d with only the common term scored %v, want in (0, 1)", position, scores[position])
		}
	}
}

func TestScores_ShorterDocWinsOnEqualCounts(test *testing.T) {
	docs := []string{
		"lease expiry",
		"lease expiry window claim retry backoff jitter timeout queue worker drain",
	}

	scores := lexical.Scores("lease", docs)

	if !(scores[0] > scores[1]) {
		test.Errorf("short doc %v should outscore long doc %v", scores[0], scores[1])
	}
}

func TestScores_RepeatedQueryTermsCountOnce(test *testing.T) {
	docs := []string{"slow reader", "slow slow writer", "fast reader"}

	once := lexical.Scores("slow reader", docs)
	twice := lexical.Scores("slow slow reader reader", docs)

	for position := range docs {
		if math.Abs(once[position]-twice[position]) > 1e-12 {
			test.Errorf("doc %d: %v with repeated terms, %v without", position, twice[position], once[position])
		}
	}
}

func TestScores_NoMatch(test *testing.T) {
	cases := []struct {
		name  string
		query string
		docs  []string
	}{
		{name: "no shared term", query: "ollama", docs: []string{"lease expiry", "hub broadcast"}},
		{name: "stopwords only", query: "what is the", docs: []string{"what is the hub"}},
		{name: "no docs", query: "hub", docs: nil},
		{name: "empty docs", query: "hub", docs: []string{"", ""}},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			if scores := lexical.Scores(testCase.query, testCase.docs); scores != nil {
				test.Errorf("Scores = %v, want nil", scores)
			}
		})
	}
}

func TestCoverage(test *testing.T) {
	cases := []struct {
		name  string
		query string
		text  string
		want  float64
	}{
		{name: "query names the whole title", query: "which topic does ledger-sync consume", text: "ledger-sync", want: 1},
		{name: "separators and case don't matter", query: "Ledger Sync topics", text: "ledger_sync", want: 1},
		{name: "inflections meet", query: "how are nodes typed", text: "Node types", want: 1},
		{name: "part of the title", query: "how does lexical fusion score passages", text: "internal/lexical — word-match scores", want: 0.4},
		{name: "repeated title words count once", query: "sync", text: "sync sync ledger", want: 0.5},
		{name: "stopwords are not title words", query: "plan", text: "The Plan", want: 1},
		{name: "no title word in the query", query: "which topic does ledger-sync consume", text: "incident 4", want: 0},
		{name: "empty title", query: "ledger-sync", text: "", want: 0},
		{name: "stopword-only query", query: "what is the", text: "the", want: 0},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			got := lexical.QueryTerms(testCase.query).Coverage(testCase.text)

			if math.Abs(got-testCase.want) > 1e-12 {
				test.Errorf("Coverage(%q, %q) = %v, want %v", testCase.query, testCase.text, got, testCase.want)
			}
		})
	}
}

// BenchmarkScores sizes the per-query cost on a large vault: 10,000 leaves of
// about 600 bytes, the median leaf size in tusk's own docs.
func BenchmarkScores(bench *testing.B) {
	sentence := "The drain claims a batch of queued rows under a lease, embeds each one, and acks it once the vector is stored. "
	docs := make([]string, 10_000)

	for position := range docs {
		docs[position] = strings.Repeat(sentence, 5) + strconv.Itoa(position)
	}

	bench.ResetTimer()

	for range bench.N {
		lexical.Scores("what happens when the lease expires during a drain", docs)
	}
}
