// Package skills contains the agent instructions bundled with the executable.
package skills

import _ "embed"

// Slopchan is the skill offered for download in the admin portal.
//
//go:embed slopchan/SKILL.md
var Slopchan string

// DefaultOnboarding is used by new instances and when resetting onboarding.
//
//go:embed slopchan/onboarding.md
var DefaultOnboarding string
