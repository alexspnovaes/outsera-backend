package movie

// Movie is one nominee (which may also be a winner) in the source CSV.
type Movie struct {
	ID        int64    `json:"id"`
	Year      int      `json:"year"`
	Title     string   `json:"title"`
	Studios   []string `json:"studios"`
	Producers []string `json:"producers"`
	Winner    bool     `json:"winner"`
}

type Filter struct {
	Year   *int
	Winner *bool
	Page   int
	Size   int
}

// The page metadata follows the familiar Spring Data response used by the UI.
type Sort struct {
	Empty    bool `json:"empty"`
	Sorted   bool `json:"sorted"`
	Unsorted bool `json:"unsorted"`
}

type Pageable struct {
	PageNumber int   `json:"pageNumber"`
	PageSize   int   `json:"pageSize"`
	Offset     int64 `json:"offset"`
	Paged      bool  `json:"paged"`
	Unpaged    bool  `json:"unpaged"`
	Sort       Sort  `json:"sort"`
}

type Page struct {
	Content          []Movie  `json:"content"`
	Pageable         Pageable `json:"pageable"`
	TotalElements    int      `json:"totalElements"`
	TotalPages       int      `json:"totalPages"`
	Last             bool     `json:"last"`
	First            bool     `json:"first"`
	Number           int      `json:"number"`
	NumberOfElements int      `json:"numberOfElements"`
	Size             int      `json:"size"`
	Sort             Sort     `json:"sort"`
}

type YearWinnerCount struct {
	Year        int `json:"year"`
	WinnerCount int `json:"winnerCount"`
}

type StudioWinCount struct {
	Name     string `json:"name"`
	WinCount int    `json:"winCount"`
}

type WinInterval struct {
	Producer     string `json:"producer"`
	Interval     int    `json:"interval"`
	PreviousWin  int    `json:"previousWin"`
	FollowingWin int    `json:"followingWin"`
}

type IntervalResult struct {
	Min []WinInterval `json:"min"`
	Max []WinInterval `json:"max"`
}

type producerWin struct {
	Producer string
	Year     int
}
