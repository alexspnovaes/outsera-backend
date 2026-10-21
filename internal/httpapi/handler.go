package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/alexspnovaes/outsera-backend/internal/movie"
)

type Handler struct {
	store   *movie.Store
	origins map[string]bool
}

func New(store *movie.Store, allowedOrigins []string) http.Handler {
	origins := make(map[string]bool, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		if origin = strings.TrimSpace(origin); origin != "" {
			origins[origin] = true
		}
	}
	return &Handler{store: store, origins: origins}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	origin := r.Header.Get("Origin")
	if origin != "" {
		w.Header().Add("Vary", "Origin")
		if h.origins[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}
	}

	path := r.URL.Path
	isID := strings.HasPrefix(path, "/api/movies/") && len(strings.TrimPrefix(path, "/api/movies/")) > 0 && !strings.Contains(strings.TrimPrefix(path, "/api/movies/"), "/")
	known := path == "/health" || path == "/api/movies" || isID
	if !known {
		writeError(w, http.StatusNotFound, "route not found")
		return
	}
	if r.Method == http.MethodOptions {
		if origin != "" && !h.origins[origin] {
			writeError(w, http.StatusForbidden, "origin not allowed")
			return
		}
		w.Header().Set("Allow", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET, OPTIONS")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	switch path {
	case "/health":
		if err := h.store.Ping(r.Context()); err != nil {
			h.internalError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	case "/api/movies":
		h.list(w, r)
	case "/api/movies/yearsWithMultipleWinners":
		if !noQuery(w, r) {
			return
		}
		years, err := h.store.YearsWithMultipleWinners(r.Context())
		if err != nil {
			h.internalError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"years": years})
	case "/api/movies/studiosWithWinCount":
		if !noQuery(w, r) {
			return
		}
		studios, err := h.store.StudiosWithWinCount(r.Context())
		if err != nil {
			h.internalError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"studios": studios})
	case "/api/movies/maxMinWinIntervalForProducers":
		if !noQuery(w, r) {
			return
		}
		intervals, err := h.store.WinIntervals(r.Context())
		if err != nil {
			h.internalError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, intervals)
	case "/api/movies/winnersByYear":
		h.winners(w, r)
	default:
		if !noQuery(w, r) {
			return
		}
		id, err := strconv.ParseInt(strings.TrimPrefix(path, "/api/movies/"), 10, 64)
		if err != nil || id < 1 {
			writeError(w, http.StatusBadRequest, "id must be a positive integer")
			return
		}
		item, err := h.store.Get(r.Context(), id)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "movie not found")
			return
		}
		if err != nil {
			h.internalError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, item)
	}
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	query, err := readQuery(r, "page", "size", "year", "winner")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	page, err := integerQuery(query, "page", 0, 0, math.MaxInt32)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	size, err := integerQuery(query, "size", 15, 1, 100)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	filter := movie.Filter{Page: page, Size: size}
	if query.Has("year") {
		year, err := integerQuery(query, "year", 0, 1, 9999)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		filter.Year = &year
	}
	if query.Has("winner") {
		value := query.Get("winner")
		if value != "true" && value != "false" {
			writeError(w, http.StatusBadRequest, "winner must be true or false")
			return
		}
		winner := value == "true"
		filter.Winner = &winner
	}
	result, err := h.store.List(r.Context(), filter)
	if err != nil {
		h.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) winners(w http.ResponseWriter, r *http.Request) {
	query, err := readQuery(r, "year")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !query.Has("year") {
		writeError(w, http.StatusBadRequest, "year is required")
		return
	}
	year, err := integerQuery(query, "year", 0, 1, 9999)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	items, err := h.store.WinnersByYear(r.Context(), year)
	if err != nil {
		h.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func readQuery(r *http.Request, allowed ...string) (url.Values, error) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return nil, fmt.Errorf("invalid query string")
	}
	for key, values := range query {
		known := false
		for _, name := range allowed {
			if key == name {
				known = true
				break
			}
		}
		if !known {
			return nil, fmt.Errorf("unknown query parameter %q", key)
		}
		if len(values) != 1 {
			return nil, fmt.Errorf("query parameter %q must appear once", key)
		}
	}
	return query, nil
}

func integerQuery(query url.Values, name string, fallback, minimum, maximum int) (int, error) {
	if !query.Has(name) {
		return fallback, nil
	}
	value, err := strconv.Atoi(query.Get(name))
	if err != nil || value < minimum || value > maximum {
		return 0, fmt.Errorf("%s must be an integer between %d and %d", name, minimum, maximum)
	}
	return value, nil
}

func noQuery(w http.ResponseWriter, r *http.Request) bool {
	if _, err := readQuery(r); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return false
	}
	return true
}

func (h *Handler) internalError(w http.ResponseWriter, err error) {
	log.Printf("API request failed: %v", err)
	writeError(w, http.StatusInternalServerError, "internal server error")
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("write response: %v", err)
	}
}
