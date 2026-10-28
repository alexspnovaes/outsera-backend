package domain

import "sort"

// ProducerWin é um prêmio de um produtor.
type ProducerWin struct {
	Producer string
	Year     int
	MovieID  int64
}

// WinInterval é a distância entre dois prêmios seguidos.
type WinInterval struct {
	Producer     string
	Interval     int
	PreviousWin  int
	FollowingWin int
}

// IntervalResult junta os empates do menor e do maior intervalo.
type IntervalResult struct {
	Min []WinInterval
	Max []WinInterval
}

// ConsecutiveWinIntervals compara só prêmios seguidos do mesmo produtor.
// A ordem é nome, ano e id do filme. Dois prêmios no mesmo ano dão intervalo 0.
// Quem tem um prêmio só fica de fora. Sem nenhum par, min e max voltam vazios.
func ConsecutiveWinIntervals(wins []ProducerWin) IntervalResult {
	ordered := append([]ProducerWin(nil), wins...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Producer != ordered[j].Producer {
			return ordered[i].Producer < ordered[j].Producer
		}
		if ordered[i].Year != ordered[j].Year {
			return ordered[i].Year < ordered[j].Year
		}
		return ordered[i].MovieID < ordered[j].MovieID
	})

	result := IntervalResult{
		Min: make([]WinInterval, 0),
		Max: make([]WinInterval, 0),
	}
	for index := 1; index < len(ordered); index++ {
		previous, current := ordered[index-1], ordered[index]
		if previous.Producer != current.Producer {
			continue
		}
		interval := WinInterval{
			Producer:     current.Producer,
			Interval:     current.Year - previous.Year,
			PreviousWin:  previous.Year,
			FollowingWin: current.Year,
		}
		if len(result.Min) == 0 || interval.Interval < result.Min[0].Interval {
			result.Min = []WinInterval{interval}
		} else if interval.Interval == result.Min[0].Interval {
			result.Min = append(result.Min, interval)
		}
		if len(result.Max) == 0 || interval.Interval > result.Max[0].Interval {
			result.Max = []WinInterval{interval}
		} else if interval.Interval == result.Max[0].Interval {
			result.Max = append(result.Max, interval)
		}
	}
	return result
}
