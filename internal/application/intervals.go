package application

import (
	"context"
	"fmt"

	"github.com/alexspnovaes/outsera-backend/internal/domain"
)

// Intervals calcula o menor e o maior intervalo entre prêmios.
type Intervals struct {
	awards AwardRepository
}

func NewIntervals(awards AwardRepository) *Intervals {
	return &Intervals{awards: awards}
}

func (u *Intervals) List(ctx context.Context) (domain.IntervalResult, error) {
	wins, err := u.awards.ListProducerWins(ctx)
	if err != nil {
		return domain.IntervalResult{}, fmt.Errorf("list producer wins: %w", err)
	}
	return domain.ConsecutiveWinIntervals(wins), nil
}
