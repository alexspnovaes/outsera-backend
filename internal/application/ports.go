package application

import (
	"context"
	"io"

	"github.com/alexspnovaes/outsera-backend/internal/domain"
)

// MovieReader lê o CSV inteiro. Arquivo inválido volta erro, sem lista pela metade.
type MovieReader interface {
	Read(source io.Reader) ([]domain.Movie, error)
}

// MovieRepository troca os filmes gravados. Se ReplaceAll falhar, os dados antigos ficam.
type MovieRepository interface {
	ReplaceAll(ctx context.Context, movies []domain.Movie) error
}

// AwardRepository lista uma linha por produtor e filme vencedor.
type AwardRepository interface {
	ListProducerWins(ctx context.Context) ([]domain.ProducerWin, error)
}
