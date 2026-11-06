package web

import (
	"bytes"
	"html/template"
	"testing"
)

func TestPageThemeColor(t *testing.T) {
	cases := map[string]template.CSS{
		"#89b4fa":            "#89b4fa",
		"#fff":               "#fff",
		"var(--icon-violet)": "var(--icon-violet)",
		"var(--blue)":        "var(--blue)",
		"":                   "",
		"red":                "",
		"var(--a); color: x": "",
		`#fff" onload="x`:    "",
		"var(--a)}body{":     "",
		"url(x)":             "",
	}
	for in, want := range cases {
		if got := pageThemeColor(in); got != want {
			t.Errorf("pageThemeColor(%q) = %q, want %q", in, got, want)
		}
	}
}

// The value must survive html/template's escaping of a style attribute unchanged.
func TestPageThemeColorInStyleAttribute(t *testing.T) {
	tmpl := template.Must(template.New("").Parse(`<html{{ with .c }} style="--primaryColor: {{ . }}"{{ end }}>`))
	for in, want := range map[string]string{
		"var(--icon-violet)": `<html style="--primaryColor: var(--icon-violet)">`,
		"#89b4fa":            `<html style="--primaryColor: #89b4fa">`,
		"bad;":               `<html>`,
	} {
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, map[string]any{"c": pageThemeColor(in)}); err != nil {
			t.Fatal(err)
		}
		if buf.String() != want {
			t.Errorf("%q rendered %q, want %q", in, buf.String(), want)
		}
	}
}
