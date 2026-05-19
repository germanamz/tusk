package index

// CLISourcePath is the synthetic source_path attributed to edges added
// via "tusk edge add" before frontmatter-backed mutation shipped. After
// the frontmatter migration, no new rows use this value; doctor migrates
// any remaining rows into source frontmatter and removes them.
const CLISourcePath = "__cli__"

// MCPSourcePath is the synthetic source_path attributed to edges added
// via the tusk_edge_add MCP tool before frontmatter-backed mutation shipped.
// See CLISourcePath for the doctor migration story.
const MCPSourcePath = "__mcp__"
