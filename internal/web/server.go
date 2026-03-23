package web

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"classact/internal/model"
	"classact/internal/scraper"
	"classact/internal/storage"
)

// Server is the local web frontend.
type Server struct {
	store    storage.Repository
	engine   *scraper.Engine
	logger   *slog.Logger
	scraping atomic.Bool // guards concurrent scrape triggers
}

// NewServer creates a new web server with the given dependencies.
func NewServer(store storage.Repository, engine *scraper.Engine, logger *slog.Logger) *Server {
	return &Server{
		store:  store,
		engine: engine,
		logger: logger,
	}
}

// Handler returns the HTTP handler with all routes registered.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /", s.handleDashboard)
	mux.HandleFunc("POST /api/scrape", s.handleTriggerScrape)
	mux.HandleFunc("GET /api/scrape/status", s.handleScrapeStatus)
	mux.HandleFunc("POST /api/applied/{id}", s.handleMarkApplied)
	mux.HandleFunc("GET /api/companies", s.handleListCompanies)
	mux.HandleFunc("POST /api/companies", s.handleAddCompany)
	mux.HandleFunc("DELETE /api/companies/{name}", s.handleRemoveCompany)
	mux.HandleFunc("GET /api/logs", s.handleLogs)

	return mux
}

// Start starts the HTTP server on the given address, blocking until ctx is cancelled.
func (s *Server) Start(ctx context.Context, addr string) error {
	srv := &http.Server{
		Addr:    addr,
		Handler: s.Handler(),
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()

	s.logger.Info("web server starting", slog.String("addr", addr))
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("web server: %w", err)
	}
	return nil
}

// writeJSON writes a JSON response with the given status code.
func (s *Server) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		s.logger.Error("failed to write JSON response", slog.String("error", err.Error()))
	}
}

// writeError writes a JSON error response.
func (s *Server) writeError(w http.ResponseWriter, status int, msg string) {
	s.writeJSON(w, status, map[string]string{"error": msg})
}

// handleDashboard serves the main dashboard page.
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	ctx := r.Context()

	lawsuits, err := s.store.ListLawsuits(ctx, storage.LawsuitFilter{})
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to list lawsuits", slog.String("error", err.Error()))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	companies, err := s.store.GetCompanyFilter(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to get company filter", slog.String("error", err.Error()))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	latestRun, err := s.store.GetLatestRunResult(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to get latest run", slog.String("error", err.Error()))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	data := dashboardData{
		Lawsuits:  lawsuits,
		Companies: companies,
		LatestRun: latestRun,
		Scraping:  s.scraping.Load(),
	}

	tmpl, err := template.New("dashboard").Funcs(template.FuncMap{
		"fmtTime": func(t time.Time) string {
			if t.IsZero() {
				return "—"
			}
			return t.Format("2006-01-02 15:04")
		},
		"fmtOptTime": func(t *time.Time) string {
			if t == nil {
				return "—"
			}
			return t.Format("2006-01-02 15:04")
		},
	}).Parse(dashboardTemplate)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to parse template", slog.String("error", err.Error()))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		s.logger.ErrorContext(ctx, "failed to execute template", slog.String("error", err.Error()))
	}
}

type dashboardData struct {
	Lawsuits  []model.LawsuitRecord
	Companies []string
	LatestRun *model.RunResult
	Scraping  bool
}

// handleTriggerScrape starts an on-demand scrape if one is not already running.
func (s *Server) handleTriggerScrape(w http.ResponseWriter, r *http.Request) {
	if !s.scraping.CompareAndSwap(false, true) {
		s.writeError(w, http.StatusConflict, "scrape already in progress")
		return
	}

	go func() {
		defer s.scraping.Store(false)
		ctx := context.Background()
		if _, err := s.engine.Run(ctx); err != nil {
			s.logger.Error("on-demand scrape failed", slog.String("error", err.Error()))
		}
	}()

	s.writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

// scrapeStatusResponse is the JSON response for GET /api/scrape/status.
type scrapeStatusResponse struct {
	Scraping  bool       `json:"scraping"`
	LatestRun *runResult `json:"latest_run,omitempty"`
}

// runResult is the JSON-serializable form of model.RunResult.
type runResult struct {
	ID             string      `json:"id"`
	StartTime      time.Time   `json:"start_time"`
	EndTime        time.Time   `json:"end_time"`
	TotalFound     int         `json:"total_found"`
	NewRecords     int         `json:"new_records"`
	UpdatedRecords int         `json:"updated_records"`
	Errors         []siteError `json:"errors,omitempty"`
}

type siteError struct {
	SiteName string `json:"site_name"`
	URL      string `json:"url"`
	Error    string `json:"error"`
}

func toRunResult(r *model.RunResult) *runResult {
	if r == nil {
		return nil
	}
	rr := &runResult{
		ID:             r.ID,
		StartTime:      r.StartTime,
		EndTime:        r.EndTime,
		TotalFound:     r.TotalFound,
		NewRecords:     r.NewRecords,
		UpdatedRecords: r.UpdatedRecords,
	}
	for _, e := range r.Errors {
		errStr := ""
		if e.Err != nil {
			errStr = e.Err.Error()
		}
		rr.Errors = append(rr.Errors, siteError{
			SiteName: e.SiteName,
			URL:      e.URL,
			Error:    errStr,
		})
	}
	return rr
}

// handleScrapeStatus returns the current scrape status and latest run result.
func (s *Server) handleScrapeStatus(w http.ResponseWriter, r *http.Request) {
	latest, err := s.store.GetLatestRunResult(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to get run status")
		return
	}

	resp := scrapeStatusResponse{
		Scraping:  s.scraping.Load(),
		LatestRun: toRunResult(latest),
	}
	s.writeJSON(w, http.StatusOK, resp)
}

// handleMarkApplied marks a lawsuit as applied.
func (s *Server) handleMarkApplied(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "missing lawsuit id")
		return
	}

	if err := s.store.MarkApplied(r.Context(), id); err != nil {
		if strings.Contains(err.Error(), "not found") {
			s.writeError(w, http.StatusNotFound, err.Error())
			return
		}
		s.writeError(w, http.StatusInternalServerError, "failed to mark applied")
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleListCompanies returns the company filter list.
func (s *Server) handleListCompanies(w http.ResponseWriter, r *http.Request) {
	companies, err := s.store.GetCompanyFilter(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to list companies")
		return
	}
	if companies == nil {
		companies = []string{}
	}
	s.writeJSON(w, http.StatusOK, companies)
}

// handleAddCompany adds a company to the filter list.
func (s *Server) handleAddCompany(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		s.writeError(w, http.StatusBadRequest, "company name cannot be empty")
		return
	}

	if err := s.store.AddCompany(r.Context(), body.Name); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to add company")
		return
	}

	s.writeJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
}

// handleRemoveCompany removes a company from the filter list.
func (s *Server) handleRemoveCompany(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		s.writeError(w, http.StatusBadRequest, "missing company name")
		return
	}

	if err := s.store.RemoveCompany(r.Context(), name); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to remove company")
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleLogs returns the latest run result as JSON (scraping logs).
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	latest, err := s.store.GetLatestRunResult(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to get logs")
		return
	}

	if latest == nil {
		s.writeJSON(w, http.StatusOK, map[string]any{"message": "no scraping runs yet"})
		return
	}

	s.writeJSON(w, http.StatusOK, toRunResult(latest))
}

const dashboardTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>ClassAct — Class Action Dashboard</title>
<style>
  body { font-family: system-ui, sans-serif; margin: 0; padding: 20px; background: #f5f5f5; color: #333; }
  h1 { margin-top: 0; }
  .container { max-width: 1200px; margin: 0 auto; }
  .card { background: #fff; border-radius: 8px; padding: 16px; margin-bottom: 16px; box-shadow: 0 1px 3px rgba(0,0,0,0.1); }
  .card h2 { margin-top: 0; font-size: 1.1em; }
  table { width: 100%; border-collapse: collapse; }
  th, td { text-align: left; padding: 8px 12px; border-bottom: 1px solid #eee; }
  th { background: #f9f9f9; font-weight: 600; }
  .badge { display: inline-block; padding: 2px 8px; border-radius: 4px; font-size: 0.85em; }
  .badge-applied { background: #d4edda; color: #155724; }
  .badge-open { background: #cce5ff; color: #004085; }
  .badge-closed { background: #f8d7da; color: #721c24; }
  button { padding: 8px 16px; border: none; border-radius: 4px; cursor: pointer; font-size: 0.95em; }
  button:disabled { opacity: 0.6; cursor: not-allowed; }
  .btn-primary { background: #0066cc; color: #fff; }
  .btn-danger { background: #dc3545; color: #fff; font-size: 0.8em; padding: 4px 8px; }
  .btn-success { background: #28a745; color: #fff; font-size: 0.8em; padding: 4px 8px; }
  .empty-state { text-align: center; padding: 40px; color: #888; }
  .status-bar { display: flex; gap: 16px; flex-wrap: wrap; }
  .status-item { font-size: 0.9em; }
  .status-item strong { display: block; font-size: 0.8em; color: #666; text-transform: uppercase; }
  .filter-form { display: flex; gap: 8px; margin-top: 8px; }
  .filter-form input { padding: 6px 10px; border: 1px solid #ccc; border-radius: 4px; }
  .filter-tags { display: flex; gap: 6px; flex-wrap: wrap; margin-top: 8px; }
  .filter-tag { display: inline-flex; align-items: center; gap: 4px; background: #e9ecef; padding: 4px 8px; border-radius: 4px; font-size: 0.85em; }
</style>
</head>
<body>
<div class="container">
  <h1>ClassAct Dashboard</h1>

  <div class="card">
    <h2>Scrape Control</h2>
    <button id="scrapeBtn" class="btn-primary" {{if .Scraping}}disabled{{end}} onclick="triggerScrape()">
      {{if .Scraping}}Scraping in progress…{{else}}Run Scrape Now{{end}}
    </button>
    {{if .LatestRun}}
    <div class="status-bar" style="margin-top:12px">
      <div class="status-item"><strong>Last Run</strong>{{fmtTime .LatestRun.StartTime}}</div>
      <div class="status-item"><strong>Ended</strong>{{fmtTime .LatestRun.EndTime}}</div>
      <div class="status-item"><strong>Found</strong>{{.LatestRun.TotalFound}}</div>
      <div class="status-item"><strong>New</strong>{{.LatestRun.NewRecords}}</div>
      <div class="status-item"><strong>Updated</strong>{{.LatestRun.UpdatedRecords}}</div>
      <div class="status-item"><strong>Errors</strong>{{len .LatestRun.Errors}}</div>
    </div>
    {{end}}
  </div>

  <div class="card">
    <h2>Company Filter</h2>
    <div class="filter-tags" id="filterTags">
      {{range .Companies}}
      <span class="filter-tag">{{.}} <button class="btn-danger" onclick="removeCompany('{{.}}')">&times;</button></span>
      {{end}}
      {{if not .Companies}}<span style="color:#888">No filters set — showing all lawsuits</span>{{end}}
    </div>
    <div class="filter-form">
      <input type="text" id="companyInput" placeholder="Add company name…">
      <button class="btn-primary" onclick="addCompany()">Add</button>
    </div>
  </div>

  <div class="card">
    <h2>Lawsuits</h2>
    {{if .Lawsuits}}
    <table>
      <thead>
        <tr>
          <th>Title</th>
          <th>Company</th>
          <th>Filing Date</th>
          <th>Deadline</th>
          <th>Status</th>
          <th>Applied</th>
          <th>Actions</th>
        </tr>
      </thead>
      <tbody>
        {{range .Lawsuits}}
        <tr>
          <td><a href="{{.SourceURL}}" target="_blank">{{.Title}}</a></td>
          <td>{{.Company}}</td>
          <td>{{fmtOptTime .FilingDate}}</td>
          <td>{{fmtOptTime .Deadline}}</td>
          <td><span class="badge badge-{{.Status}}">{{.Status}}</span></td>
          <td>{{if .Applied}}<span class="badge badge-applied">Applied</span>{{else}}—{{end}}</td>
          <td>{{if not .Applied}}<button class="btn-success" onclick="markApplied('{{.ID}}')">Mark Applied</button>{{end}}</td>
        </tr>
        {{end}}
      </tbody>
    </table>
    {{else}}
    <div class="empty-state">
      <p>No lawsuits found. Click "Run Scrape Now" to discover class action lawsuits.</p>
    </div>
    {{end}}
  </div>
</div>

<script>
function triggerScrape() {
  var btn = document.getElementById('scrapeBtn');
  btn.disabled = true;
  btn.textContent = 'Scraping in progress…';
  fetch('/api/scrape', {method:'POST'}).then(function(r) {
    if (r.status === 409) { alert('A scrape is already running.'); return; }
    pollStatus();
  });
}
function pollStatus() {
  var iv = setInterval(function() {
    fetch('/api/scrape/status').then(function(r){return r.json()}).then(function(d) {
      if (!d.scraping) { clearInterval(iv); location.reload(); }
    });
  }, 2000);
}
function markApplied(id) {
  fetch('/api/applied/'+id, {method:'POST'}).then(function() { location.reload(); });
}
function addCompany() {
  var input = document.getElementById('companyInput');
  var name = input.value.trim();
  if (!name) return;
  fetch('/api/companies', {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({name:name})})
    .then(function() { location.reload(); });
}
function removeCompany(name) {
  fetch('/api/companies/'+encodeURIComponent(name), {method:'DELETE'}).then(function() { location.reload(); });
}
</script>
</body>
</html>`
