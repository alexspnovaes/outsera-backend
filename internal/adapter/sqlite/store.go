package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/alexspnovaes/outsera-backend/internal/application"
	"github.com/alexspnovaes/outsera-backend/internal/domain"

	_ "modernc.org/sqlite"
)

var (
	_ application.MovieRepository = (*Store)(nil)
	_ application.AwardRepository = (*Store)(nil)
)

var databaseSequence atomic.Uint64

const schema = `
CREATE TABLE movies (
	id INTEGER PRIMARY KEY,
	year INTEGER NOT NULL CHECK (year BETWEEN 1 AND 9999),
	title TEXT NOT NULL,
	winner INTEGER NOT NULL CHECK (winner IN (0, 1))
);
CREATE TABLE studios (
	id INTEGER PRIMARY KEY,
	name TEXT NOT NULL UNIQUE
);
CREATE TABLE producers (
	id INTEGER PRIMARY KEY,
	name TEXT NOT NULL UNIQUE
);
CREATE TABLE movie_studios (
	movie_id INTEGER NOT NULL REFERENCES movies(id) ON DELETE CASCADE,
	studio_id INTEGER NOT NULL REFERENCES studios(id),
	position INTEGER NOT NULL,
	PRIMARY KEY (movie_id, studio_id)
);
CREATE TABLE movie_producers (
	movie_id INTEGER NOT NULL REFERENCES movies(id) ON DELETE CASCADE,
	producer_id INTEGER NOT NULL REFERENCES producers(id),
	position INTEGER NOT NULL,
	PRIMARY KEY (movie_id, producer_id)
);
CREATE INDEX movies_winner ON movies(winner);
CREATE INDEX movie_producers_producer ON movie_producers(producer_id);
`

// Store é um SQLite só deste processo, em memória.
type Store struct {
	db *sql.DB
}

// Open cria um banco vazio. O nome muda a cada chamada para as instâncias não compartilharem dados.
// O pool usa uma conexão, suficiente para o catálogo pequeno e imutável após a inicialização.
func Open(ctx context.Context) (*Store, error) {
	dsn := fmt.Sprintf("file:outsera-%d?mode=memory&cache=shared&_pragma=foreign_keys(1)", databaseSequence.Add(1))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(ctx); err != nil {
		return nil, closeAfterError(db, fmt.Errorf("connect database: %w", err))
	}
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		return nil, closeAfterError(db, fmt.Errorf("enable foreign keys: %w", err))
	}
	if _, err := db.ExecContext(ctx, schema); err != nil {
		return nil, closeAfterError(db, fmt.Errorf("create database schema: %w", err))
	}
	return &Store{db: db}, nil
}

func closeAfterError(db *sql.DB, cause error) error {
	if err := db.Close(); err != nil {
		return errors.Join(cause, fmt.Errorf("close database: %w", err))
	}
	return cause
}

func (s *Store) Close() error {
	return s.db.Close()
}

// ReplaceAll troca todos os filmes numa transação. Falha no meio desfaz a troca.
func (s *Store) ReplaceAll(ctx context.Context, movies []domain.Movie) error {
	for index, movie := range movies {
		if err := movie.Validate(); err != nil {
			return fmt.Errorf("movie %d: %w", index+1, err)
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin import: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, "DELETE FROM movies; DELETE FROM studios; DELETE FROM producers;"); err != nil {
		return fmt.Errorf("clear previous dataset: %w", err)
	}
	for index, movie := range movies {
		id := int64(index + 1)
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO movies(id, year, title, winner) VALUES (?, ?, ?, ?)",
			id, movie.Year, movie.Title, movie.Winner,
		); err != nil {
			return fmt.Errorf("insert movie %d: %w", id, err)
		}
		if err := insertNames(ctx, tx, id, kindStudio, movie.Studios); err != nil {
			return err
		}
		if err := insertNames(ctx, tx, id, kindProducer, movie.Producers); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit import: %w", err)
	}
	return nil
}

type nameKind int

const (
	kindStudio nameKind = iota
	kindProducer
)

func insertNames(ctx context.Context, tx *sql.Tx, movieID int64, kind nameKind, names []string) error {
	insertName := "INSERT INTO studios(name) VALUES (?) ON CONFLICT(name) DO NOTHING"
	linkName := "INSERT INTO movie_studios(movie_id, studio_id, position) SELECT ?, id, ? FROM studios WHERE name = ?"
	label := "studio"
	if kind == kindProducer {
		insertName = "INSERT INTO producers(name) VALUES (?) ON CONFLICT(name) DO NOTHING"
		linkName = "INSERT INTO movie_producers(movie_id, producer_id, position) SELECT ?, id, ? FROM producers WHERE name = ?"
		label = "producer"
	}
	for position, name := range names {
		if _, err := tx.ExecContext(ctx, insertName, name); err != nil {
			return fmt.Errorf("insert %s: %w", label, err)
		}
		if _, err := tx.ExecContext(ctx, linkName, movieID, position, name); err != nil {
			return fmt.Errorf("link %s: %w", label, err)
		}
	}
	return nil
}

// ListProducerWins devolve os prêmios. A ordenação da conta também acontece no domínio.
func (s *Store) ListProducerWins(ctx context.Context) ([]domain.ProducerWin, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.name, m.year, m.id
		FROM producers p
		JOIN movie_producers mp ON mp.producer_id = p.id
		JOIN movies m ON m.id = mp.movie_id
		WHERE m.winner = 1
		ORDER BY p.name, m.year, m.id`)
	if err != nil {
		return nil, fmt.Errorf("query producer wins: %w", err)
	}
	defer rows.Close()

	wins := make([]domain.ProducerWin, 0)
	for rows.Next() {
		var win domain.ProducerWin
		if err := rows.Scan(&win.Producer, &win.Year, &win.MovieID); err != nil {
			return nil, fmt.Errorf("scan producer win: %w", err)
		}
		wins = append(wins, win)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate producer wins: %w", err)
	}
	return wins, nil
}
