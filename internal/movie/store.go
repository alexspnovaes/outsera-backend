package movie

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"strings"
	"sync/atomic"

	_ "modernc.org/sqlite"
)

var databaseSequence atomic.Uint64

type Store struct {
	db *sql.DB
}

// NewStore creates an isolated in-memory database. Each process imports its CSV
// at startup; no external database server or persisted database is required.
func NewStore(ctx context.Context) (*Store, error) {
	dsn := fmt.Sprintf("file:outsera-%d?mode=memory&cache=shared&_pragma=foreign_keys(1)", databaseSequence.Add(1))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db}
	_, err = db.ExecContext(ctx, `
		CREATE TABLE movies (
			id INTEGER PRIMARY KEY,
			year INTEGER NOT NULL CHECK (year BETWEEN 1 AND 9999),
			title TEXT NOT NULL,
			winner INTEGER NOT NULL CHECK (winner IN (0, 1))
		);
		CREATE INDEX movies_year_winner ON movies(year, winner);
		CREATE TABLE studios (id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE);
		CREATE TABLE producers (id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE);
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
	`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("create database schema: %w", err)
	}
	return store, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

// ImportCSV replaces the complete dataset atomically, preserving the previous
// dataset if validation or any database insert fails.
func (s *Store) ImportCSV(ctx context.Context, source io.Reader) (int, error) {
	movies, err := ReadCSV(source)
	if err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin import: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "DELETE FROM movies; DELETE FROM studios; DELETE FROM producers;"); err != nil {
		return 0, fmt.Errorf("clear previous dataset: %w", err)
	}
	for index, item := range movies {
		id := int64(index + 1)
		if _, err := tx.ExecContext(ctx, "INSERT INTO movies(id, year, title, winner) VALUES (?, ?, ?, ?)", id, item.Year, item.Title, item.Winner); err != nil {
			return 0, fmt.Errorf("insert movie %d: %w", id, err)
		}
		if err := insertNames(ctx, tx, id, "studios", "movie_studios", "studio_id", item.Studios); err != nil {
			return 0, err
		}
		if err := insertNames(ctx, tx, id, "producers", "movie_producers", "producer_id", item.Producers); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit import: %w", err)
	}
	return len(movies), nil
}

// Identifiers here are constants controlled by this package, never user input.
func insertNames(ctx context.Context, tx *sql.Tx, movieID int64, table, relation, foreignKey string, names []string) error {
	for position, name := range names {
		if _, err := tx.ExecContext(ctx, "INSERT INTO "+table+"(name) VALUES (?) ON CONFLICT(name) DO NOTHING", name); err != nil {
			return fmt.Errorf("insert %s: %w", table, err)
		}
		query := "INSERT INTO " + relation + "(movie_id, " + foreignKey + ", position) SELECT ?, id, ? FROM " + table + " WHERE name = ?"
		if _, err := tx.ExecContext(ctx, query, movieID, position, name); err != nil {
			return fmt.Errorf("link %s: %w", table, err)
		}
	}
	return nil
}

func (s *Store) List(ctx context.Context, filter Filter) (Page, error) {
	where := make([]string, 0, 2)
	args := make([]any, 0, 4)
	if filter.Year != nil {
		where = append(where, "year = ?")
		args = append(args, *filter.Year)
	}
	if filter.Winner != nil {
		where = append(where, "winner = ?")
		args = append(args, *filter.Winner)
	}
	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}
	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM movies"+clause, args...).Scan(&total); err != nil {
		return Page{}, err
	}
	offset := int64(filter.Page) * int64(filter.Size)
	args = append(args, filter.Size, offset)
	content, err := s.queryMovies(ctx, "SELECT id, year, title, winner FROM movies"+clause+" ORDER BY id LIMIT ? OFFSET ?", args...)
	if err != nil {
		return Page{}, err
	}
	totalPages := (total + filter.Size - 1) / filter.Size
	sort := Sort{Empty: true, Unsorted: true}
	return Page{
		Content: content, TotalElements: total, TotalPages: totalPages,
		Last: filter.Page >= totalPages-1, First: filter.Page == 0,
		Number: filter.Page, NumberOfElements: len(content), Size: filter.Size, Sort: sort,
		Pageable: Pageable{PageNumber: filter.Page, PageSize: filter.Size, Offset: offset, Paged: true, Sort: sort},
	}, nil
}

func (s *Store) Get(ctx context.Context, id int64) (Movie, error) {
	movies, err := s.queryMovies(ctx, "SELECT id, year, title, winner FROM movies WHERE id = ?", id)
	if err != nil {
		return Movie{}, err
	}
	if len(movies) == 0 {
		return Movie{}, sql.ErrNoRows
	}
	return movies[0], nil
}

func (s *Store) WinnersByYear(ctx context.Context, year int) ([]Movie, error) {
	return s.queryMovies(ctx, "SELECT id, year, title, winner FROM movies WHERE year = ? AND winner = 1 ORDER BY id", year)
}

func (s *Store) queryMovies(ctx context.Context, query string, args ...any) ([]Movie, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	movies := make([]Movie, 0)
	for rows.Next() {
		var item Movie
		if err := rows.Scan(&item.ID, &item.Year, &item.Title, &item.Winner); err != nil {
			rows.Close()
			return nil, err
		}
		movies = append(movies, item)
	}
	err = rows.Err()
	rows.Close() // Release the connection before reading each movie's names.
	if err != nil {
		return nil, err
	}
	for index := range movies {
		item := &movies[index]
		item.Studios, err = s.names(ctx, item.ID, "studios", "movie_studios", "studio_id")
		if err != nil {
			return nil, err
		}
		item.Producers, err = s.names(ctx, item.ID, "producers", "movie_producers", "producer_id")
		if err != nil {
			return nil, err
		}
	}
	return movies, nil
}

func (s *Store) names(ctx context.Context, movieID int64, table, relation, foreignKey string) ([]string, error) {
	query := "SELECT n.name FROM " + table + " n JOIN " + relation + " r ON r." + foreignKey + " = n.id WHERE r.movie_id = ? ORDER BY r.position"
	rows, err := s.db.QueryContext(ctx, query, movieID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := make([]string, 0)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

func (s *Store) YearsWithMultipleWinners(ctx context.Context) ([]YearWinnerCount, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT year, COUNT(*) FROM movies WHERE winner = 1 GROUP BY year HAVING COUNT(*) > 1 ORDER BY year")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	years := make([]YearWinnerCount, 0)
	for rows.Next() {
		var year YearWinnerCount
		if err := rows.Scan(&year.Year, &year.WinnerCount); err != nil {
			return nil, err
		}
		years = append(years, year)
	}
	return years, rows.Err()
}

func (s *Store) StudiosWithWinCount(ctx context.Context) ([]StudioWinCount, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.name, COUNT(*) AS win_count
		FROM studios s JOIN movie_studios ms ON ms.studio_id = s.id
		JOIN movies m ON m.id = ms.movie_id
		WHERE m.winner = 1 GROUP BY s.id, s.name
		ORDER BY win_count DESC, s.name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	studios := make([]StudioWinCount, 0)
	for rows.Next() {
		var studio StudioWinCount
		if err := rows.Scan(&studio.Name, &studio.WinCount); err != nil {
			return nil, err
		}
		studios = append(studios, studio)
	}
	return studios, rows.Err()
}

func (s *Store) producerWins(ctx context.Context) ([]producerWin, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.name, m.year
		FROM producers p JOIN movie_producers mp ON mp.producer_id = p.id
		JOIN movies m ON m.id = mp.movie_id
		WHERE m.winner = 1 ORDER BY p.name, m.year, m.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	wins := make([]producerWin, 0)
	for rows.Next() {
		var win producerWin
		if err := rows.Scan(&win.Producer, &win.Year); err != nil {
			return nil, err
		}
		wins = append(wins, win)
	}
	return wins, rows.Err()
}
