---
type: package
title: internal/linenum — line numbering schemes
import-path: github.com/germanamz/tusk/internal/linenum
status: stable
---

# internal/linenum

Turns byte offsets in a file into 1-based line numbers under a configurable scheme. Tools disagree about what ends a line, so the scheme exists to make the line ranges tusk reports match the tool a reader opens the file with. It never affects parsing: markdown is always parsed by CommonMark rules.

## Public surface

- `Scheme` and its values:
  - `SchemeLF` (`lf`, the default): only LF ends a line, so CRLF counts once and a lone CR is text. Matches sed, grep -n, wc -l, cat -n and file-read tools.
  - `SchemeUniversal` (`universal`): adds a lone CR, the CommonMark line endings most editors follow.
  - `SchemeUnicode` (`unicode`): adds VT, FF, NEL (U+0085), LS (U+2028) and PS (U+2029), the UAX #14 mandatory breaks.
- `ParseScheme(value) (Scheme, error)` — resolves a manifest value; empty is the default, anything else outside the three is an error.
- `NewTable(source, scheme) *Table` and `(*Table).Line(offset) int` — one scan per file, then a binary search per offset. A terminator's bytes belong to the line they end.

## Notes

The schemes agree on LF and CRLF files and differ only on lone CRs and the Unicode separators. `internal/manifest` validates `[workspace] line-numbering` with `ParseScheme`; `internal/subunit.AssignLines` numbers sub-unit spans with a `Table`.
