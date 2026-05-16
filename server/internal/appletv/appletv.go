package appletv

import (
	"net/http"
)

// MainHandler serves the root navigation (https://<host>/).
func (g *XMLGenerator) MainHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		g.RenderError(w, r, ErrorData{Title: "Method Not Allowed", Description: r.Method})
		return
	}
	g.Render(w, r, "main.xml", nil)
}
