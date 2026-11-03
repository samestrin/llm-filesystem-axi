// Package skill carries the Agent Skill that sits beside it, so
// `llm-filesystem setup` installs the same bytes this repository publishes.
//
// The file is embedded rather than copied into a Go string. A second copy is a
// second thing to keep in step, and the skill is edited often enough that the
// two would drift.
package skill

import _ "embed"

// Content is SKILL.md, byte for byte.
//
//go:embed SKILL.md
var Content string
