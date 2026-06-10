// Package htmltext renders HTML source to deterministic plain prose. It is a
// dependency-light leaf package (only golang.org/x/net/html) so the same
// normalizer can be shared by internal/htmlunit, internal/node, and the render
// path without forming an import cycle — internal/subunit imports internal/node
// and internal/htmlunit imports internal/subunit, so internal/node cannot import
// internal/htmlunit directly.
package htmltext
