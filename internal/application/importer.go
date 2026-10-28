package application

import (
	"context"
	"fmt"
	"io"
)

// Importer grava o CSV no banco na subida da API.
type Importer struct {
	reader  MovieReader
	catalog MovieRepository
}

func NewImporter(reader MovieReader, catalog MovieRepository) *Importer {
	return &Importer{reader: reader, catalog: catalog}
}

// Import lê o arquivo e só então substitui os filmes. Erro de leitura não grava nada.
func (u *Importer) Import(ctx context.Context, source io.Reader) (int, error) {
	movies, err := u.reader.Read(source)
	if err != nil {
		return 0, fmt.Errorf("read movies: %w", err)
	}
	if err := u.catalog.ReplaceAll(ctx, movies); err != nil {
		return 0, fmt.Errorf("store movies: %w", err)
	}
	return len(movies), nil
}
