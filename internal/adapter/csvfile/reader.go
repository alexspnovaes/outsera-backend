package csvfile

import (
	"bufio"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/alexspnovaes/outsera-backend/internal/application"
	"github.com/alexspnovaes/outsera-backend/internal/domain"
)

// Reader lê o CSV do desafio.
type Reader struct{}

var _ application.MovieReader = Reader{}

// MaxFileSize é o maior arquivo aceito na importação.
const MaxFileSize int64 = 8 << 20

// Read devolve todos os filmes ou um erro. Linha inválida descarta o arquivo inteiro.
func (Reader) Read(source io.Reader) ([]domain.Movie, error) {
	limited := &io.LimitedReader{R: source, N: MaxFileSize + 1}
	buffered := bufio.NewReader(limited)
	prefix, err := buffered.Peek(3)
	if err == nil && string(prefix) == "\xef\xbb\xbf" {
		if _, err := buffered.Discard(3); err != nil {
			return nil, fmt.Errorf("read CSV header: %w", err)
		}
	}

	reader := csv.NewReader(buffered)
	reader.Comma = ';'
	reader.FieldsPerRecord = 5
	header, err := reader.Read()
	if limited.N == 0 {
		return nil, fmt.Errorf("CSV exceeds the %d-byte limit", MaxFileSize)
	}
	if err != nil {
		return nil, fmt.Errorf("read CSV header: %w", err)
	}
	expected := []string{"year", "title", "studios", "producers", "winner"}
	for index, name := range expected {
		value := strings.TrimSpace(header[index])
		if index == 0 {
			value = strings.TrimPrefix(value, "\ufeff")
		}
		if strings.ToLower(value) != name {
			return nil, fmt.Errorf("CSV column %d: expected %q, got %s", index+1, name, quotedValue(value))
		}
	}

	movies := make([]domain.Movie, 0)
	for {
		record, err := reader.Read()
		if limited.N == 0 {
			return nil, fmt.Errorf("CSV exceeds the %d-byte limit", MaxFileSize)
		}
		if errors.Is(err, io.EOF) {
			return movies, nil
		}
		if err != nil {
			return nil, fmt.Errorf("read CSV record: %w", err)
		}
		line, _ := reader.FieldPos(0)
		movie, err := parseRecord(line, record)
		if err != nil {
			return nil, err
		}
		movies = append(movies, movie)
	}
}

func parseRecord(line int, record []string) (domain.Movie, error) {
	year, err := strconv.Atoi(strings.TrimSpace(record[0]))
	if err != nil || year < 1 || year > 9999 {
		return domain.Movie{}, fmt.Errorf("CSV line %d: invalid year %s", line, quotedValue(record[0]))
	}
	title := strings.TrimSpace(record[1])
	studios := splitNames(record[2], false)
	producers := splitNames(record[3], true)
	winner, err := parseWinner(line, record[4])
	if err != nil {
		return domain.Movie{}, err
	}
	movie := domain.Movie{
		Year:      year,
		Title:     title,
		Studios:   studios,
		Producers: producers,
		Winner:    winner,
	}
	if err := movie.Validate(); err != nil {
		return domain.Movie{}, fmt.Errorf("CSV line %d: %w", line, err)
	}
	return movie, nil
}

func parseWinner(line int, value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "yes":
		return true, nil
	case "", "no":
		return false, nil
	default:
		return false, fmt.Errorf("CSV line %d: invalid winner %s (expected yes, no or empty)", line, quotedValue(value))
	}
}

// splitNames separa produtores em " and ", preserva essa expressão em estúdios
// e elimina nomes repetidos dentro do mesmo filme.
func splitNames(value string, splitAnd bool) []string {
	if splitAnd {
		value = strings.ReplaceAll(value, " and ", ",")
	}
	names := make([]string, 0)
	seen := make(map[string]struct{})
	for _, part := range strings.Split(value, ",") {
		name := strings.TrimSpace(part)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		names = append(names, name)
		seen[name] = struct{}{}
	}
	return names
}

func quotedValue(value string) string {
	const maxRunes = 80
	valid := []rune(strings.ToValidUTF8(value, "�"))
	if len(valid) > maxRunes {
		return strconv.Quote(string(valid[:maxRunes]) + "…")
	}
	return strconv.Quote(string(valid))
}
