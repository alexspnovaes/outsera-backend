package sqlite

import (
	"context"
	"strings"
	"testing"

	"github.com/alexspnovaes/outsera-backend/internal/adapter/csvfile"
	"github.com/alexspnovaes/outsera-backend/internal/domain"
)

func TestImportPersistsNormalizedMovies(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	source := "\ufeffyear;title;studios;producers;winner\n" +
		"2001;\"Replacement; subtitle\";Studio and Partners, Other Studio;Alice, Alice, Bob, and Charlie;no\n"
	movies, err := csvfile.Reader{}.Read(strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceAll(ctx, movies); err != nil {
		t.Fatal(err)
	}

	var enabled int
	if err := store.db.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if enabled != 1 {
		t.Fatalf("foreign_keys=%d", enabled)
	}

	var year int
	var title string
	var winner bool
	if err := store.db.QueryRowContext(ctx, "SELECT year, title, winner FROM movies").Scan(&year, &title, &winner); err != nil {
		t.Fatal(err)
	}
	if year != 2001 || title != "Replacement; subtitle" || winner {
		t.Fatalf("movie year=%d title=%q winner=%v", year, title, winner)
	}
	studios := linkedNames(t, store, `
		SELECT s.name FROM studios s
		JOIN movie_studios ms ON ms.studio_id = s.id
		ORDER BY ms.position`)
	if !equal(studios, []string{"Studio and Partners", "Other Studio"}) {
		t.Fatalf("studios=%v", studios)
	}
	producers := linkedNames(t, store, `
		SELECT p.name FROM producers p
		JOIN movie_producers mp ON mp.producer_id = p.id
		ORDER BY mp.position`)
	if !equal(producers, []string{"Alice", "Bob", "Charlie"}) {
		t.Fatalf("producers=%v", producers)
	}

	wins, err := store.ListProducerWins(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(wins) != 0 {
		t.Fatalf("non-winner produced awards: %+v", wins)
	}

	replacement := []domain.Movie{{
		Year: 2010, Title: "Only winner", Studios: []string{"Studio"}, Producers: []string{"Ada"}, Winner: true,
	}}
	if err := store.ReplaceAll(ctx, replacement); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM movies").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("movies=%d after replace, want 1", count)
	}
	wins, err = store.ListProducerWins(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(wins) != 1 || wins[0].Producer != "Ada" || wins[0].Year != 2010 {
		t.Fatalf("wins=%+v", wins)
	}
	var producerCount int
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM producers").Scan(&producerCount); err != nil {
		t.Fatal(err)
	}
	if producerCount != 1 {
		t.Fatalf("producers=%d after replace, want 1", producerCount)
	}
}

func TestFailedReplaceKeepsPreviousCatalog(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	original := []domain.Movie{{
		Year: 2000, Title: "Original", Studios: []string{"Studio"}, Producers: []string{"Ada"}, Winner: true,
	}}
	if err := store.ReplaceAll(ctx, original); err != nil {
		t.Fatal(err)
	}

	invalid := []domain.Movie{{
		Year: 0, Title: "Broken", Studios: []string{"Studio"}, Producers: []string{"Zoe"}, Winner: true,
	}}
	if err := store.ReplaceAll(ctx, invalid); err == nil {
		t.Fatal("invalid catalog was stored")
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	replacement := []domain.Movie{{
		Year: 2011, Title: "Replacement", Studios: []string{"Studio"}, Producers: []string{"Zoe"}, Winner: true,
	}}
	if err := store.ReplaceAll(cancelled, replacement); err == nil {
		t.Fatal("cancelled replace succeeded")
	}

	if _, err := store.db.ExecContext(ctx, `
		CREATE TRIGGER reject_replacement
		BEFORE INSERT ON movies
		WHEN NEW.title = 'Forced failure'
		BEGIN
			SELECT RAISE(ABORT, 'forced failure');
		END`); err != nil {
		t.Fatal(err)
	}
	failedDuringWrite := []domain.Movie{{
		Year: 2012, Title: "Forced failure", Studios: []string{"Studio"}, Producers: []string{"Zoe"}, Winner: true,
	}}
	if err := store.ReplaceAll(ctx, failedDuringWrite); err == nil {
		t.Fatal("replace that failed during the transaction succeeded")
	}

	var title string
	if err := store.db.QueryRowContext(ctx, "SELECT title FROM movies").Scan(&title); err != nil {
		t.Fatal(err)
	}
	if title != "Original" {
		t.Fatalf("title=%q", title)
	}
}

func openStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func linkedNames(t *testing.T, store *Store, query string) []string {
	t.Helper()
	rows, err := store.db.QueryContext(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	found := make([]string, 0)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		found = append(found, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return found
}

func equal(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}
