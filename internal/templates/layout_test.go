package templates

import (
	"strings"
	"testing"
	"time"
)

// The plain style follows the reader's colour scheme; the branded card and a custom layout stay light.
func TestRenderEmailColorScheme(t *testing.T) {
	c := &Context{Values: vals(map[string]string{}), Location: time.UTC, BrandColor: "#2563EB", BrandName: "Acme"}
	parts := Parts{Content: "<p>Hi</p>", ContentText: "Hi", Signature: "<p>Alex</p>"}

	d := DefaultDesign()
	d.Layout.Style = "plain"
	e := c.RenderEmail(d, parts)
	if !strings.Contains(e.HTML, `content="light dark"`) || strings.Contains(e.HTML, "light only") {
		t.Errorf("plain should allow dark mode: %s", e.HTML)
	}
	for _, fixed := range []string{"color:#171717", "color:#404040", "padding:16px 20px"} {
		if strings.Contains(e.HTML, fixed) {
			t.Errorf("plain sets %q", fixed)
		}
	}

	d.Layout.Style = "branded"
	e = c.RenderEmail(d, parts)
	if !strings.Contains(e.HTML, `content="light only"`) || !strings.Contains(e.HTML, "color:#171717") {
		t.Errorf("branded stays light: %s", e.HTML)
	}

	d.Layout.Mode = "custom"
	d.CustomHTML = SanitizeDesign(`<div>{{ content }}</div>`)
	if e = c.RenderEmail(d, parts); !strings.Contains(e.HTML, `content="light only"`) {
		t.Errorf("custom stays light: %s", e.HTML)
	}
}
