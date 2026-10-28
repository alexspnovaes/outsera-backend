package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/alexspnovaes/outsera-backend/internal/domain"
)

// PrizeIntervalPath é a rota do desafio.
const PrizeIntervalPath = "/api/movies/maxMinWinIntervalForProducers"

// IntervalLister é o que o handler precisa para montar a resposta.
type IntervalLister interface {
	List(context.Context) (domain.IntervalResult, error)
}

type handler struct {
	intervals IntervalLister
}

func New(intervals IntervalLister) http.Handler {
	return handler{intervals: intervals}
}

func (h handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != PrizeIntervalPath {
		writeJSON(w, http.StatusNotFound, errorResponse{Error: "route not found"})
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		h.serveIntervals(w, r)
	default:
		w.Header().Set("Allow", "GET, HEAD")
		writeJSON(w, http.StatusMethodNotAllowed, errorResponse{Error: "method not allowed"})
	}
}

func (h handler) serveIntervals(w http.ResponseWriter, r *http.Request) {
	result, err := h.intervals.List(r.Context())
	if err != nil {
		slog.Error("prize intervals", "err", err)
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal server error"})
		return
	}
	writeJSON(w, http.StatusOK, intervalResponse{
		Min: toDTO(result.Min),
		Max: toDTO(result.Max),
	})
}

type intervalDTO struct {
	Producer     string `json:"producer"`
	Interval     int    `json:"interval"`
	PreviousWin  int    `json:"previousWin"`
	FollowingWin int    `json:"followingWin"`
}

type intervalResponse struct {
	Min []intervalDTO `json:"min"`
	Max []intervalDTO `json:"max"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func toDTO(items []domain.WinInterval) []intervalDTO {
	out := make([]intervalDTO, 0, len(items))
	for _, item := range items {
		out = append(out, intervalDTO{
			Producer:     item.Producer,
			Interval:     item.Interval,
			PreviousWin:  item.PreviousWin,
			FollowingWin: item.FollowingWin,
		})
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		slog.Error("write json response", "err", err)
	}
}
