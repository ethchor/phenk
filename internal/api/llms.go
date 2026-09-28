package api

import (
	"bytes"
	_ "embed"
	"net/http"
	"strings"
	"text/template"

	"github.com/ethchor/phenk/internal/core"
	"github.com/ethchor/phenk/internal/store/pg"
)

// llmsSource is the agent-facing guide served at /llms.txt.
//
// It follows the llms.txt convention: a plain-text page, written for a language
// model, that says what the site is and how to use it. An agent that has only
// ever been told "use Phenk to get a verification email" can read this one
// page and complete the whole flow. It is text/template rather than a static
// file so that the domain, the base URL and the limits in it are this server's
// real ones.
//
//go:embed llms.txt
var llmsSource string

var llmsTemplate = template.Must(template.New("llms").Parse(llmsSource))

// llmsTxt serves the agent guide.
func (s *Server) llmsTxt(w http.ResponseWriter, r *http.Request) {
	domain := "your-public-domain.example"
	domains, err := pg.AllocatableDomains(r.Context(), s.db, core.PoolPublic)
	if err != nil {
		internalError(w, r, "listing domains for llms.txt", err)
		return
	}
	if len(domains) > 0 {
		domain = domains[0].Name
	}

	var out bytes.Buffer
	err = llmsTemplate.Execute(&out, struct {
		BaseURL        string
		Domain         string
		RetentionHours int
		MaxWaitSeconds int
	}{
		BaseURL:        strings.TrimRight(s.cfg.PublicURL, "/"),
		Domain:         domain,
		RetentionHours: int(s.cfg.PublicRetention.Hours()),
		MaxWaitSeconds: int(s.cfg.MaxWaitTimeout.Seconds()),
	})
	if err != nil {
		internalError(w, r, "rendering llms.txt", err)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(out.Bytes())
}
