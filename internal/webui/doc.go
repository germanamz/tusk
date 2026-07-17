// Package webui hosts the shared HTTP-serving scaffold behind the local,
// read-only vault view commands (`tusk graph`, `tusk book`): a DNS-rebinding
// Host-header guard, a generic SSE broadcast hub, the reindex/epoch change
// source, and an embedded-asset static handler. It never opens the workspace.
package webui
