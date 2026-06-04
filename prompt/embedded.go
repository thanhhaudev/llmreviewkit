package prompt

import (
	"embed"
)

//go:embed embedded/review.md embedded/adversarial-review.md
var embeddedFS embed.FS

// embeddedTemplate returns the bundled default template for the given mode,
// or "" + false if no template is embedded. Used by Build when caller
// doesn't supply a pluginRoot.
func embeddedTemplate(mode Mode) (string, bool) {
	name := "embedded/" + mode.TemplateFile()
	data, err := embeddedFS.ReadFile(name)
	if err != nil {
		return "", false
	}
	return string(data), true
}
