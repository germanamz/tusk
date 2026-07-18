// encodeId percent-encodes each path segment of a node id while keeping real
// "/" separators intact, so the Go {id...} multi-segment wildcard still captures
// the whole id. Without this a "#" in a sub-unit id (`<fileID>#<address>`) is
// read as a URL fragment and stripped, and spaces malform the path. The Go route
// unescapes each segment via PathValue, so the id round-trips.
export function encodeId(id: string): string {
  return id.split('/').map(encodeURIComponent).join('/')
}
