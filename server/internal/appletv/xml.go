package appletv

import (
	"embed"
	"net/http"
	"text/template"

	"github.com/atv-media-server/server/internal/logging"
)

const (
	baseXML  = "templates/base.xml"
	errorXML = "templates/error.xml"
)

//go:embed templates
var templates embed.FS

// TemplateData is passed to every page template.
type TemplateData struct {
	BasePath string // e.g. "https://appletv.redbull.tv" — for XML pages and assets
	BaseHost string // e.g. "appletv.redbull.tv"        — for HTTP media URLs where AVPlayer rejects HTTPS
	BodyID   string
	Data     interface{}
}

// ErrorData is the payload for error.xml.
type ErrorData struct {
	Title       string
	Description string
}

// XMLGenerator renders ATV3 XML templates with a base host injected into URLs.
type XMLGenerator struct {
	baseHost string
}

// New returns an XMLGenerator scoped to a host such as "appletv.redbull.tv".
func New(baseHost string) *XMLGenerator {
	return &XMLGenerator{baseHost: baseHost}
}

func (g *XMLGenerator) basePath() string {
	return "https://" + g.baseHost
}

// Render parses base+page templates and writes XML to w. On failure renders an error page.
func (g *XMLGenerator) Render(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
	tmpl, err := template.ParseFS(templates, baseXML, "templates/"+page)
	if err != nil {
		logging.Warn("template parse:", err)
		g.RenderError(w, r, ErrorData{Title: "Template Parse Error", Description: err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	td := TemplateData{
		BasePath: g.basePath(),
		BaseHost: g.baseHost,
		BodyID:   page,
		Data:     data,
	}
	if err := tmpl.Execute(w, td); err != nil {
		logging.Warn("template execute:", err)
	}
}

// RenderError renders error.xml; if that itself fails, we just log.
func (g *XMLGenerator) RenderError(w http.ResponseWriter, r *http.Request, ed ErrorData) {
	tmpl, err := template.ParseFS(templates, baseXML, errorXML)
	if err != nil {
		logging.Warn("error template parse:", err)
		http.Error(w, ed.Title+": "+ed.Description, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	td := TemplateData{
		BasePath: g.basePath(),
		BodyID:   "templates/error.xml",
		Data:     ed,
	}
	if err := tmpl.Execute(w, td); err != nil {
		logging.Warn("error template execute:", err)
	}
}
