package movie

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// ReadCSV validates the entire source before any database change is made.
func ReadCSV(source io.Reader) ([]Movie, error) {
	buffered := bufio.NewReader(source)
	// Remove the BOM before CSV parsing so a quoted first header also works.
	if prefix, _ := buffered.Peek(3); string(prefix) == "\xef\xbb\xbf" {
		_, _ = buffered.Discard(3)
	}
	reader := csv.NewReader(buffered)
	reader.Comma = ';'
	reader.FieldsPerRecord = 5
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("read CSV header: %w", err)
	}
	expected := []string{"year", "title", "studios", "producers", "winner"}
	for i, name := range expected {
		value := strings.TrimSpace(header[i])
		if i == 0 {
			value = strings.TrimPrefix(value, "\ufeff")
		}
		if strings.ToLower(value) != name {
			return nil, fmt.Errorf("CSV column %d: expected %q, got %q", i+1, name, value)
		}
	}

	movies := make([]Movie, 0)
	for {
		record, err := reader.Read()
		if err == io.EOF {
			return movies, nil
		}
		if err != nil {
			return nil, fmt.Errorf("read CSV record: %w", err)
		}
		line, _ := reader.FieldPos(0)
		year, err := strconv.Atoi(strings.TrimSpace(record[0]))
		if err != nil || year < 1 || year > 9999 {
			return nil, fmt.Errorf("CSV line %d: invalid year %q", line, record[0])
		}
		title := strings.TrimSpace(record[1])
		studios := splitNames(record[2], false)
		producers := splitNames(record[3], true)
		if title == "" || len(studios) == 0 || len(producers) == 0 {
			return nil, fmt.Errorf("CSV line %d: title, studios and producers are required", line)
		}
		winner := false
		switch strings.ToLower(strings.TrimSpace(record[4])) {
		case "yes":
			winner = true
		case "", "no":
		default:
			return nil, fmt.Errorf("CSV line %d: invalid winner %q (expected yes, no or empty)", line, record[4])
		}
		movies = append(movies, Movie{Year: year, Title: title, Studios: studios, Producers: producers, Winner: winner})
	}
}

func splitNames(value string, splitAnd bool) []string {
	if splitAnd {
		value = strings.ReplaceAll(value, " and ", ",")
	}
	names := make([]string, 0)
	seen := make(map[string]bool)
	for _, part := range strings.Split(value, ",") {
		name := strings.TrimSpace(part)
		if name != "" && !seen[name] {
			names = append(names, name)
			seen[name] = true
		}
	}
	return names
}
