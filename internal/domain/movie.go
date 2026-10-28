package domain

import (
	"errors"
	"fmt"
	"strings"
)

// Movie é um filme lido do CSV.
type Movie struct {
	Year      int
	Title     string
	Studios   []string
	Producers []string
	Winner    bool
}

// Validate recusa filme sem título, sem estúdio, sem produtor ou com ano fora de 1 a 9999.
func (m Movie) Validate() error {
	if m.Year < 1 || m.Year > 9999 {
		return fmt.Errorf("year %d is outside 1 and 9999", m.Year)
	}
	if strings.TrimSpace(m.Title) == "" {
		return errors.New("title is required")
	}
	if err := requireNames("studio", m.Studios); err != nil {
		return err
	}
	return requireNames("producer", m.Producers)
}

func requireNames(kind string, names []string) error {
	if len(names) == 0 {
		return fmt.Errorf("at least one %s is required", kind)
	}
	for _, name := range names {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("%s name cannot be blank", kind)
		}
	}
	return nil
}
