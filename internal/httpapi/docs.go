package httpapi

import (
	"fmt"
	"net/http"

	"github.com/harlew-dev/lewbox/internal/docs"
	"github.com/harlew-dev/lewbox/internal/service"
)

// serveDocs registers one documentation book's nav and page routes. A nil
// site answers 404.
func serveDocs(mux *http.ServeMux, base string, site *docs.Site) {
	mux.HandleFunc("GET "+base, func(w http.ResponseWriter, r *http.Request) {
		if site == nil {
			writeJSON(w, http.StatusNotFound, docsUnavailable())
			return
		}
		writeJSON(w, http.StatusOK, site.Nav())
	})

	mux.HandleFunc("GET "+base+"/{path...}", func(w http.ResponseWriter, r *http.Request) {
		if site == nil {
			writeJSON(w, http.StatusNotFound, docsUnavailable())
			return
		}
		requested := r.PathValue("path")
		page, ok := site.Page(requested)
		if !ok {
			writeJSON(w, http.StatusNotFound, ErrorBody{Error: ErrorDetail{
				Code:    string(service.CodeNotFound),
				Message: fmt.Sprintf("no docs page %q", requested),
			}})
			return
		}
		writeJSON(w, http.StatusOK, page)
	})
}

// docsUnavailable is the 404 body when the router has no docs site.
func docsUnavailable() ErrorBody {
	return ErrorBody{Error: ErrorDetail{
		Code:    string(service.CodeNotFound),
		Message: "docs are not available",
	}}
}
