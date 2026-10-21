package movie

import "context"

// WinIntervals compares consecutive awards for each producer. Two winning
// movies in the same year are two awards and therefore have interval zero.
// SQL orders awards by producer, year and movie ID for deterministic ties.
func (s *Store) WinIntervals(ctx context.Context) (IntervalResult, error) {
	wins, err := s.producerWins(ctx)
	result := IntervalResult{Min: make([]WinInterval, 0), Max: make([]WinInterval, 0)}
	if err != nil {
		return result, err
	}
	for index := 1; index < len(wins); index++ {
		previous, current := wins[index-1], wins[index]
		if previous.Producer != current.Producer {
			continue
		}
		interval := WinInterval{Producer: current.Producer, Interval: current.Year - previous.Year, PreviousWin: previous.Year, FollowingWin: current.Year}
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
	return result, nil
}
