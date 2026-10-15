// Package lexical scores how well a set of texts match a query's words, for
// blending with cosine similarity in semantic search.
//
// Embeddings match meaning but blur exact wording: a clause buried in a long
// passage barely moves the passage's vector, and identifier-heavy text scores
// moderately against almost anything. A word-overlap score catches what the
// vectors miss. Scores is Okapi BM25 over the texts it is handed, so it needs
// no index: term statistics come from the candidate set itself.
package lexical

import (
	"math"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// BM25 parameters: termSaturation (k1) caps how much a repeated term adds,
// and lengthPenalty (b) sets how much a long text is discounted. Both are the
// usual defaults.
const (
	termSaturation = 1.2
	lengthPenalty  = 0.75
)

// stopwords are common English function words. They carry no topic, so they
// neither count toward a match nor toward a text's length.
var stopwords = wordSet(`
	a all also an and any are as at be but by can do does each every for from
	has have how i if in into is it its no not of on only or so than that the
	their them then there these they this those to was what when where which
	who why will with you your
`)

// longestStopword lets a word longer than every stopword skip the lookup.
var longestStopword = func() int {
	longest := 0

	for word := range stopwords {
		longest = max(longest, len(word))
	}

	return longest
}()

func wordSet(words string) map[string]struct{} {
	set := make(map[string]struct{})

	for _, word := range strings.Fields(words) {
		set[word] = struct{}{}
	}

	return set
}

func isStopword(word string) bool {
	if len(word) > longestStopword {
		return false
	}

	_, stop := stopwords[word]

	return stop
}

// suffixes are stripped from a token, first match wins, so inflections of a
// word meet: "dropped" and "dropping" both become "dropp", "slowly" becomes
// "slow". Deliberately cruder than a real stemmer; it only has to make a
// query word and a text word land on the same string.
var suffixes = []string{"ingly", "edly", "ing", "ers", "er", "ed", "ly", "es", "s"}

// minStem is the shortest stem a suffix may leave behind, so "bus" and "red"
// stay whole.
const minStem = 3

// Tokens splits text into the words Scores compares: lowercase runs of
// letters and digits, minus stopwords, each with one common suffix stripped.
func Tokens(text string) []string {
	var tokens []string

	eachWord(text, func(word string) {
		tokens = append(tokens, stem(word))
	})

	return tokens
}

// eachWord hands yield each lowercase non-stopword word of text in order,
// unstemmed. It runs once per candidate per query, so it scans text in place
// and allocates only to lowercase a word that has an uppercase letter.
func eachWord(text string, yield func(string)) {
	start := -1
	folded := false

	emit := func(end int) {
		word := text[start:end]

		if folded {
			word = strings.ToLower(word)
		}

		if !isStopword(word) {
			yield(word)
		}

		start = -1
		folded = false
	}

	for offset, char := range text {
		var isWord, isUpper bool

		switch {
		case char >= 'a' && char <= 'z', char >= '0' && char <= '9':
			isWord = true
		case char >= 'A' && char <= 'Z':
			isWord, isUpper = true, true
		case char >= utf8.RuneSelf:
			isWord = unicode.IsLetter(char) || unicode.IsDigit(char)
			isUpper = isWord && unicode.ToLower(char) != char
		}

		switch {
		case isWord && start < 0:
			start, folded = offset, isUpper
		case isWord:
			folded = folded || isUpper
		case start >= 0:
			emit(offset)
		}
	}

	if start >= 0 {
		emit(len(text))
	}
}

func stem(token string) string {
	for _, suffix := range suffixes {
		if strings.HasSuffix(token, suffix) && len(token)-len(suffix) >= minStem {
			return token[:len(token)-len(suffix)]
		}
	}

	return token
}

// Scores returns the BM25 score of each doc against query, divided by the
// best doc's score: the best match scores 1 and a doc sharing no word with
// the query scores 0. Term rarity and average length are taken from docs
// alone. A word repeated in the query counts once. It returns nil when no doc
// shares a word with the query.
func Scores(query string, docs []string) []float64 {
	var terms []string

	for _, token := range Tokens(query) {
		if !slices.Contains(terms, token) {
			terms = append(terms, token)
		}
	}

	if len(terms) == 0 || len(docs) == 0 {
		return nil
	}

	// counts[doc][term] is how often a query term occurs in a doc; it stays
	// nil for a doc with no query term, which is most of them.
	counts := make([][]int, len(docs))
	lengths := make([]int, len(docs))
	docFrequency := make([]int, len(terms))
	totalLength := 0

	for position, doc := range docs {
		eachWord(doc, func(word string) {
			lengths[position]++

			term := matchTerm(word, terms)

			if term < 0 {
				return
			}

			if counts[position] == nil {
				counts[position] = make([]int, len(terms))
			}

			if counts[position][term] == 0 {
				docFrequency[term]++
			}

			counts[position][term]++
		})

		totalLength += lengths[position]
	}

	if totalLength == 0 {
		return nil
	}

	docCount := float64(len(docs))
	averageLength := float64(totalLength) / docCount
	inverseFrequency := make([]float64, len(terms))

	for term, frequency := range docFrequency {
		inverseFrequency[term] = math.Log(1 + (docCount-float64(frequency)+0.5)/(float64(frequency)+0.5))
	}

	scores := make([]float64, len(docs))
	best := 0.0

	for position, termCounts := range counts {
		if termCounts == nil {
			continue
		}

		saturation := termSaturation * (1 - lengthPenalty + lengthPenalty*float64(lengths[position])/averageLength)

		for term, count := range termCounts {
			if count == 0 {
				continue
			}

			frequency := float64(count)
			scores[position] += inverseFrequency[term] * frequency * (termSaturation + 1) / (frequency + saturation)
		}

		best = max(best, scores[position])
	}

	if best <= 0 {
		return nil
	}

	for position := range scores {
		scores[position] /= best
	}

	return scores
}

// matchTerm returns the index of the query term word stems to, or -1. A stem
// is a prefix of its word, so only a word starting with some term is
// stemmed; on a large vault that skips nearly every word.
func matchTerm(word string, terms []string) int {
	stemmed := ""

	for term, queryTerm := range terms {
		if !strings.HasPrefix(word, queryTerm) {
			continue
		}

		if stemmed == "" {
			stemmed = stem(word)
		}

		if stemmed == queryTerm {
			return term
		}
	}

	return -1
}
