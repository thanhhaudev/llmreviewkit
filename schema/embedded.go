package schema

import (
	"embed"
)

//go:embed embedded/review-output.schema.json
var embeddedFS embed.FS

// embeddedSchemaJSON returns the bundled default schema as a string,
// or "" + false if the embedded file is unreachable (would only happen
// if the build dropped the embed directive — practically never).
func embeddedSchemaJSON() (string, bool) {
	data, err := embeddedFS.ReadFile("embedded/review-output.schema.json")
	if err != nil {
		return "", false
	}
	return string(data), true
}
