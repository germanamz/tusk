package doctor

import (
	"fmt"
	"strings"
)

// representPrefix is the query instruction mxbai-embed-large and
// snowflake-arctic-embed (v1) are trained with.
const representPrefix = "Represent this sentence for searching relevant passages: "

// prefixFamily is an embedding model family trained with instruction
// prefixes, matched by model-name prefix (and, when marker is set, a
// substring that tells a version apart). An empty documentPrefix means the
// family prefixes queries only. Strings come from each model's card.
type prefixFamily struct {
	name           string
	marker         string
	queryPrefix    string
	documentPrefix string
}

// prefixFamilies is checked in order and the first match wins, so both v2
// spellings of snowflake-arctic-embed (Ollama's snowflake-arctic-embed2 and
// Hugging Face's snowflake-arctic-embed-l-v2.0) precede the v1 entry whose
// prefix they share.
var prefixFamilies = []prefixFamily{
	{name: "nomic-embed-text", queryPrefix: "search_query: ", documentPrefix: "search_document: "},
	{name: "embeddinggemma", queryPrefix: "task: search result | query: ", documentPrefix: "title: {title} | text: "},
	{name: "mxbai-embed-large", queryPrefix: representPrefix},
	{name: "snowflake-arctic-embed2", queryPrefix: "query: "},
	{name: "snowflake-arctic-embed", marker: "-v2", queryPrefix: "query: "},
	{name: "snowflake-arctic-embed", queryPrefix: representPrefix},
}

// matchPrefixFamily finds the family of an Ollama model reference, comparing
// the last path segment (hf.co/org/name:tag -> name) up to its tag,
// case-insensitively.
func matchPrefixFamily(model string) (prefixFamily, bool) {
	base := strings.ToLower(model[strings.LastIndex(model, "/")+1:])
	base, _, _ = strings.Cut(base, ":")

	for _, family := range prefixFamilies {
		if strings.HasPrefix(base, family.name) && strings.Contains(base, family.marker) {
			return family, true
		}
	}

	return prefixFamily{}, false
}

// checkEmbeddingPrefixHint advises setting the instruction prefixes a known
// asymmetric model expects. For a family with prefixes on both sides it
// fires when either is unset: with none, it names both; with one, it names the
// missing side, since a prefix on one side only leaves queries and documents
// embedded under different instructions. For a query-only family it fires
// when the query prefix is unset. The gain depends on the vault, so the hint
// suggests measuring rather than promising better results. No-op when
// [embeddings] is unconfigured.
func checkEmbeddingPrefixHint(config Config) ([]Issue, error) {
	if config.Manifest == nil || config.Manifest.Embeddings.Provider == "" {
		return nil, nil
	}

	section := config.Manifest.Embeddings
	family, known := matchPrefixFamily(section.Model)

	if !known {
		return nil, nil
	}

	var message string

	switch {
	case family.documentPrefix != "" && section.QueryPrefix == "" && section.DocumentPrefix == "":
		message = fmt.Sprintf("model %q is trained with instruction prefixes; consider query-prefix = %q and document-prefix = %q under [embeddings] (from the model card). Results vary by vault, so compare a few semantic queries before and after; changing document-prefix re-embeds on the next reindex.",
			section.Model, family.queryPrefix, family.documentPrefix)
	case family.documentPrefix != "" && section.DocumentPrefix == "":
		message = fmt.Sprintf("model %q is trained with instruction prefixes on both sides, but only query-prefix is set; consider document-prefix = %q under [embeddings] (from the model card) so documents match the query side. Changing document-prefix re-embeds on the next reindex.",
			section.Model, family.documentPrefix)
	case family.documentPrefix != "" && section.QueryPrefix == "":
		message = fmt.Sprintf("model %q is trained with instruction prefixes on both sides, but only document-prefix is set; consider query-prefix = %q under [embeddings] (from the model card) so queries match the document side. Changing query-prefix re-embeds nothing.",
			section.Model, family.queryPrefix)
	case family.documentPrefix == "" && section.QueryPrefix == "":
		message = fmt.Sprintf("model %q is trained with a query instruction prefix; consider query-prefix = %q under [embeddings] (from the model card; documents take no prefix). Changing query-prefix re-embeds nothing.",
			section.Model, family.queryPrefix)
	default:
		return nil, nil
	}

	return []Issue{{Kind: IssueEmbeddingPrefixHint, Message: message}}, nil
}
