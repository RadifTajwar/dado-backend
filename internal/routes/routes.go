// Package routes is the single list of every API route. It mirrors the table
// in frontend/lib/api/endpoints.ts, in the same order.
package routes

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"dado/internal/auth"
	"dado/internal/handlers"
)

func New(h *handlers.Handler, jwtSecret []byte) http.Handler {
	mux := http.NewServeMux()

	loginLimit := auth.NewLimiter(10, 15*time.Minute)
	forgotLimit := auth.NewLimiter(5, time.Hour)
	resetLimit := auth.NewLimiter(10, time.Hour)
	formLimit := auth.NewLimiter(10, time.Hour)
	limit := func(l *auth.Limiter, msg string, fn http.HandlerFunc) http.Handler { return l.Limit(msg, fn) }
	const tooManyForms = "Too many submissions from your connection. Please try again in an hour."

	// ---- Public ----
	mux.HandleFunc("GET /api/health", h.Health)
	mux.HandleFunc("GET /api/services", h.PublicServices)
	mux.HandleFunc("GET /api/media", h.ListMedia)
	mux.HandleFunc("GET /api/portfolio", h.PublicPortfolio)
	mux.Handle("POST /api/contacts", limit(formLimit, tooManyForms, h.CreateContact))
	mux.Handle("POST /api/trials", limit(formLimit, tooManyForms, h.CreateTrial))

	// ---- Auth ----
	mux.Handle("POST /api/auth/login", limit(loginLimit, "Too many sign-in attempts. Try again in 15 minutes.", h.Login))
	mux.HandleFunc("POST /api/auth/logout", h.Logout)
	mux.Handle("GET /api/auth/me", auth.RequireAdmin(jwtSecret, http.HandlerFunc(h.Me)))
	mux.Handle("POST /api/auth/forgot", limit(forgotLimit, "Too many reset requests. Try again in an hour.", h.ForgotPassword))
	mux.Handle("POST /api/auth/reset", limit(resetLimit, "Too many attempts. Try again in an hour.", h.ResetPassword))

	// ---- Admin: everything under /api/admin/ needs a valid session ----
	admin := http.NewServeMux()
	admin.HandleFunc("GET /api/admin/counts", h.Counts)

	admin.HandleFunc("GET /api/admin/services", h.ListServices)
	admin.HandleFunc("POST /api/admin/services", h.CreateService)
	admin.HandleFunc("PUT /api/admin/services/{id}", h.UpdateService)
	admin.HandleFunc("DELETE /api/admin/services/{id}", h.DeleteService)

	admin.HandleFunc("GET /api/admin/clients", h.ListClients)
	admin.HandleFunc("POST /api/admin/clients", h.CreateClient)
	admin.HandleFunc("PUT /api/admin/clients/{id}", h.UpdateClient)
	admin.HandleFunc("DELETE /api/admin/clients/{id}", h.DeleteClient)

	admin.HandleFunc("GET /api/admin/jobs", h.ListJobs)
	admin.HandleFunc("POST /api/admin/jobs", h.CreateJob)
	admin.HandleFunc("PATCH /api/admin/jobs/{id}", h.PatchJob)
	admin.HandleFunc("DELETE /api/admin/jobs/{id}", h.DeleteJob)

	admin.HandleFunc("GET /api/admin/trials", h.ListTrials)
	admin.HandleFunc("PATCH /api/admin/trials/{id}", h.PatchTrial)
	admin.HandleFunc("POST /api/admin/trials/{id}/convert", h.ConvertTrial)
	admin.HandleFunc("POST /api/admin/trials/{id}/email", h.EmailTrial)

	admin.HandleFunc("GET /api/admin/contacts", h.ListContacts)
	admin.HandleFunc("PATCH /api/admin/contacts/{id}", h.PatchContact)
	admin.HandleFunc("POST /api/admin/contacts/{id}/email", h.EmailContact)

	admin.HandleFunc("GET /api/admin/portfolio", h.ListPortfolio)
	admin.HandleFunc("POST /api/admin/portfolio", h.CreatePortfolioItem)
	admin.HandleFunc("PUT /api/admin/portfolio/{id}", h.UpdatePortfolioItem)
	admin.HandleFunc("DELETE /api/admin/portfolio/{id}", h.DeletePortfolioItem)

	admin.HandleFunc("PUT /api/admin/media/{slot}", h.PutMedia)
	admin.HandleFunc("POST /api/admin/uploads", h.Upload)

	admin.HandleFunc("/api/admin/", notFound)
	mux.Handle("/api/admin/", auth.RequireAdmin(jwtSecret, admin))

	mux.HandleFunc("/", notFound) // anything else answers in the same JSON shape

	return recoverPanics(logRequests(mux))
}

func notFound(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "Not found."})
}

// statusRecorder remembers the status code so it can be logged.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		slog.Info("request", "method", r.Method, "path", r.URL.Path, "status", rec.status,
			"ms", time.Since(start).Milliseconds())
	})
}

// recoverPanics turns a crash inside a handler into a JSON 500 instead of a
// dropped connection.
func recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				slog.Error("panic", "path", r.URL.Path, "err", err)
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "Something went wrong on our side. Please try again."})
			}
		}()
		next.ServeHTTP(w, r)
	})
}
