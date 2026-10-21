package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/alexspnovaes/outsera-backend/internal/httpapi"
	"github.com/alexspnovaes/outsera-backend/internal/movie"
)

const csvHeader = "year;title;studios;producers;winner\n"

type testAPI struct {
	server *httptest.Server
	store  *movie.Store
}

func startAPI(t *testing.T, source io.Reader) testAPI {
	t.Helper()
	store, err := movie.NewStore(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	if _, err := store.ImportCSV(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.New(store, []string{"http://localhost:5173"}))
	t.Cleanup(server.Close)
	return testAPI{server: server, store: store}
}

func request(t *testing.T, api testAPI, method, path, origin string) (int, http.Header, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, api.server.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	response, err := api.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, response.Header, body
}

func getJSON(t *testing.T, api testAPI, path string, result any) {
	t.Helper()
	status, header, body := request(t, api, http.MethodGet, path, "")
	if status != http.StatusOK {
		t.Fatalf("GET %s: status=%d body=%s", path, status, body)
	}
	if !strings.HasPrefix(header.Get("Content-Type"), "application/json") {
		t.Fatalf("GET %s: unexpected Content-Type %q", path, header.Get("Content-Type"))
	}
	if err := json.Unmarshal(body, result); err != nil {
		t.Fatalf("GET %s: decode %s: %v", path, body, err)
	}
}

func TestProvidedCSVContract(t *testing.T) {
	file, err := os.Open("../../data/Movielist.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	api := startAPI(t, file)

	t.Run("health and complete import", func(t *testing.T) {
		var health map[string]string
		getJSON(t, api, "/health", &health)
		if health["status"] != "ok" {
			t.Fatalf("health=%v", health)
		}
		var page movie.Page
		getJSON(t, api, "/api/movies", &page)
		if page.TotalElements != 206 || page.TotalPages != 14 || len(page.Content) != 15 || !page.First || page.Last || page.Number != 0 || page.Size != 15 {
			t.Fatalf("unexpected page metadata: %+v", page)
		}
		if page.Content[0].Title != "Can't Stop the Music" || page.Content[0].ID != 1 || !page.Content[0].Winner {
			t.Fatalf("unexpected first movie: %+v", page.Content[0])
		}
	})
	t.Run("filters pagination and resource", func(t *testing.T) {
		var first, second movie.Page
		getJSON(t, api, "/api/movies?year=1990&winner=true&size=1", &first)
		getJSON(t, api, "/api/movies?year=1990&winner=true&size=1&page=1", &second)
		if first.TotalElements != 2 || first.TotalPages != 2 || len(first.Content) != 1 || first.Last || !second.Last || second.First || second.NumberOfElements != 1 || second.Pageable.Offset != 1 {
			t.Fatalf("incorrect filtered pagination: first=%+v second=%+v", first, second)
		}
		if first.Content[0].ID == second.Content[0].ID || second.Content[0].Year != 1990 || !second.Content[0].Winner {
			t.Fatalf("incorrect filtered movies: %+v %+v", first.Content, second.Content)
		}
		var losers movie.Page
		getJSON(t, api, "/api/movies?year=1980&winner=false", &losers)
		if losers.TotalElements != 9 {
			t.Fatalf("1980 non-winners=%d, want 9", losers.TotalElements)
		}
		var item movie.Movie
		getJSON(t, api, "/api/movies/1", &item)
		if item.Title != "Can't Stop the Music" || !reflect.DeepEqual(item.Producers, []string{"Allan Carr"}) {
			t.Fatalf("unexpected resource: %+v", item)
		}
		var outOfRange movie.Page
		getJSON(t, api, "/api/movies?page=999", &outOfRange)
		if outOfRange.Content == nil || len(outOfRange.Content) != 0 || !outOfRange.Last || outOfRange.TotalElements != 206 {
			t.Fatalf("incorrect out-of-range page: %+v", outOfRange)
		}
	})
	t.Run("multiple winners and studio ranking", func(t *testing.T) {
		var years struct {
			Years []movie.YearWinnerCount `json:"years"`
		}
		getJSON(t, api, "/api/movies/yearsWithMultipleWinners", &years)
		expectedYears := []movie.YearWinnerCount{{Year: 1986, WinnerCount: 2}, {Year: 1990, WinnerCount: 2}, {Year: 2015, WinnerCount: 2}}
		if !reflect.DeepEqual(years.Years, expectedYears) {
			t.Fatalf("years=%+v, want %+v", years.Years, expectedYears)
		}
		var studios struct {
			Studios []movie.StudioWinCount `json:"studios"`
		}
		getJSON(t, api, "/api/movies/studiosWithWinCount", &studios)
		expectedTop := []movie.StudioWinCount{{Name: "Columbia Pictures", WinCount: 7}, {Name: "Paramount Pictures", WinCount: 6}, {Name: "Warner Bros.", WinCount: 5}}
		if len(studios.Studios) < 3 || !reflect.DeepEqual(studios.Studios[:3], expectedTop) {
			t.Fatalf("studios=%+v, want top %+v", studios.Studios, expectedTop)
		}
	})
	t.Run("producer intervals and winners by year", func(t *testing.T) {
		var intervals movie.IntervalResult
		getJSON(t, api, "/api/movies/maxMinWinIntervalForProducers", &intervals)
		expected := movie.IntervalResult{
			Min: []movie.WinInterval{{Producer: "Joel Silver", Interval: 1, PreviousWin: 1990, FollowingWin: 1991}},
			Max: []movie.WinInterval{{Producer: "Matthew Vaughn", Interval: 13, PreviousWin: 2002, FollowingWin: 2015}},
		}
		if !reflect.DeepEqual(intervals, expected) {
			t.Fatalf("intervals=%+v, want %+v", intervals, expected)
		}
		var winners []movie.Movie
		getJSON(t, api, "/api/movies/winnersByYear?year=2018", &winners)
		if len(winners) != 1 || winners[0].Title != "Holmes & Watson" || !winners[0].Winner || !reflect.DeepEqual(winners[0].Producers, []string{"Will Ferrell", "Adam McKay", "Jimmy Miller", "Clayton Townsend"}) {
			t.Fatalf("2018 winners=%+v", winners)
		}
		var winnerPage movie.Page
		getJSON(t, api, "/api/movies?winner=true&size=100", &winnerPage)
		if winnerPage.TotalElements != 42 {
			t.Fatalf("winners=%d, want 42", winnerPage.TotalElements)
		}
	})
}

func TestConsecutiveIntervalsReturnAllTies(t *testing.T) {
	// Unsorted source, three wins for Alpha, two ties in each result, and a
	// repeated producer within one film. Only consecutive awards are compared.
	source := csvHeader +
		"2008;A third;Studio Z;Alpha;yes\n" +
		"2000;A first;Studio Z;Alpha, Alpha;yes\n" +
		"2002;A second;Studio A;Alpha and Beta;yes\n" +
		"2004;B second;Studio A;Beta;yes\n" +
		"1995;G first;Studio B;Gamma;yes\n" +
		"2001;G second;Studio B;Gamma;yes\n" +
		"2003;Non winner;Unused;Only Nominee;\n"
	api := startAPI(t, strings.NewReader(source))
	var result movie.IntervalResult
	getJSON(t, api, "/api/movies/maxMinWinIntervalForProducers", &result)
	expected := movie.IntervalResult{
		Min: []movie.WinInterval{
			{Producer: "Alpha", Interval: 2, PreviousWin: 2000, FollowingWin: 2002},
			{Producer: "Beta", Interval: 2, PreviousWin: 2002, FollowingWin: 2004},
		},
		Max: []movie.WinInterval{
			{Producer: "Alpha", Interval: 6, PreviousWin: 2002, FollowingWin: 2008},
			{Producer: "Gamma", Interval: 6, PreviousWin: 1995, FollowingWin: 2001},
		},
	}
	if !reflect.DeepEqual(result, expected) {
		t.Fatalf("intervals=%+v, want %+v", result, expected)
	}
	var ranking struct {
		Studios []movie.StudioWinCount `json:"studios"`
	}
	getJSON(t, api, "/api/movies/studiosWithWinCount", &ranking)
	if !reflect.DeepEqual(ranking.Studios, []movie.StudioWinCount{{Name: "Studio A", WinCount: 2}, {Name: "Studio B", WinCount: 2}, {Name: "Studio Z", WinCount: 2}}) {
		t.Fatalf("studio tie order/count=%+v", ranking.Studios)
	}
}

func TestDifferentWinningMoviesInSameYearHaveZeroInterval(t *testing.T) {
	api := startAPI(t, strings.NewReader(csvHeader+
		"2004;Later win;Studio;Alpha;yes\n"+
		"2000;First win;Studio;Alpha, Alpha;yes\n"+
		"2000;Another winning movie;Studio;Alpha;yes\n"))
	var result movie.IntervalResult
	getJSON(t, api, "/api/movies/maxMinWinIntervalForProducers", &result)
	expected := movie.IntervalResult{
		Min: []movie.WinInterval{{Producer: "Alpha", Interval: 0, PreviousWin: 2000, FollowingWin: 2000}},
		Max: []movie.WinInterval{{Producer: "Alpha", Interval: 4, PreviousWin: 2000, FollowingWin: 2004}},
	}
	if !reflect.DeepEqual(result, expected) {
		t.Fatalf("intervals=%+v, want %+v", result, expected)
	}
}

func TestEmptyResultsAreArrays(t *testing.T) {
	for _, source := range []string{csvHeader, csvHeader + "2000;Only award;Studio;Producer;yes\n"} {
		api := startAPI(t, strings.NewReader(source))
		var intervals movie.IntervalResult
		getJSON(t, api, "/api/movies/maxMinWinIntervalForProducers", &intervals)
		if intervals.Min == nil || intervals.Max == nil || len(intervals.Min) != 0 || len(intervals.Max) != 0 {
			t.Fatalf("empty intervals must be arrays: %+v", intervals)
		}
		var page movie.Page
		getJSON(t, api, "/api/movies?year=2025", &page)
		if page.Content == nil || len(page.Content) != 0 || page.TotalPages != 0 || !page.First || !page.Last {
			t.Fatalf("empty page=%+v", page)
		}
		var winners []movie.Movie
		getJSON(t, api, "/api/movies/winnersByYear?year=2025", &winners)
		if winners == nil || len(winners) != 0 {
			t.Fatalf("empty winners must be []: %+v", winners)
		}
		var years struct {
			Years []movie.YearWinnerCount `json:"years"`
		}
		getJSON(t, api, "/api/movies/yearsWithMultipleWinners", &years)
		if years.Years == nil || len(years.Years) != 0 {
			t.Fatalf("empty years must be []: %+v", years)
		}
	}
}

func TestImportValidationPreservesPreviousDataset(t *testing.T) {
	api := startAPI(t, strings.NewReader(csvHeader+"2000;Original;Studio;Producer;yes\n"))
	cases := map[string]string{
		"empty file":         "",
		"wrong header":       "year;name;studios;producers;winner\n",
		"invalid year":       csvHeader + "not-a-year;Invalid;Studio;Producer;yes\n",
		"year out of range":  csvHeader + "0;Invalid;Studio;Producer;yes\n",
		"missing column":     csvHeader + "2000;Invalid;Studio;Producer\n",
		"empty title":        csvHeader + "2000;;Studio;Producer;yes\n",
		"empty studio":       csvHeader + "2000;Invalid;;Producer;yes\n",
		"empty producer":     csvHeader + "2000;Invalid;Studio;;yes\n",
		"invalid winner":     csvHeader + "2000;Invalid;Studio;Producer;maybe\n",
		"malformed quoting":  csvHeader + "2000;\"Invalid;Studio;Producer;yes\n",
		"partial valid file": csvHeader + "2001;Would replace;Studio;Producer;yes\ninvalid;Bad;Studio;Producer;yes\n",
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := api.store.ImportCSV(context.Background(), strings.NewReader(source)); err == nil {
				t.Fatal("invalid import unexpectedly succeeded")
			}
			var page movie.Page
			getJSON(t, api, "/api/movies", &page)
			if page.TotalElements != 1 || len(page.Content) != 1 || page.Content[0].Title != "Original" {
				t.Fatalf("failed import changed previous data: %+v", page)
			}
		})
	}
	t.Run("BOM Oxford comma and studio name", func(t *testing.T) {
		source := "\ufeff\"year\";\"title\";\"studios\";\"producers\";\"winner\"\n" +
			"2001;\"Replacement; subtitle\";Studio and Partners, Other Studio;Alice, Bob, and Charlie;no\n"
		if count, err := api.store.ImportCSV(context.Background(), strings.NewReader(source)); err != nil || count != 1 {
			t.Fatalf("valid import count=%d error=%v", count, err)
		}
		var item movie.Movie
		getJSON(t, api, "/api/movies/1", &item)
		if item.Title != "Replacement; subtitle" || item.Winner || !reflect.DeepEqual(item.Studios, []string{"Studio and Partners", "Other Studio"}) || !reflect.DeepEqual(item.Producers, []string{"Alice", "Bob", "Charlie"}) {
			t.Fatalf("unexpected parsed movie: %+v", item)
		}
	})
}

func TestHTTPValidationMethodsAndCORS(t *testing.T) {
	api := startAPI(t, strings.NewReader(csvHeader+"2000;Movie;Studio;Producer;yes\n"))
	cases := []struct {
		method string
		path   string
		status int
	}{
		{"GET", "/api/movies?page=-1", 400},
		{"GET", "/api/movies?page=2147483648", 400},
		{"GET", "/api/movies?page=x", 400},
		{"GET", "/api/movies?page=0&page=1", 400},
		{"GET", "/api/movies?size=0", 400},
		{"GET", "/api/movies?size=101", 400},
		{"GET", "/api/movies?size=", 400},
		{"GET", "/api/movies?year=abc", 400},
		{"GET", "/api/movies?year=0", 400},
		{"GET", "/api/movies?year=10000", 400},
		{"GET", "/api/movies?winner=yes", 400},
		{"GET", "/api/movies?unknown=true", 400},
		{"GET", "/api/movies/winnersByYear", 400},
		{"GET", "/api/movies/winnersByYear?year=0", 400},
		{"GET", "/api/movies/studiosWithWinCount?size=5", 400},
		{"GET", "/api/movies/0", 400},
		{"GET", "/api/movies/abc", 400},
		{"GET", "/api/movies/999", 404},
		{"GET", "/api/movies/1/extra", 404},
		{"GET", "/missing", 404},
		{"POST", "/api/movies", 405},
		{"PUT", "/api/movies/1", 405},
		{"DELETE", "/api/movies/maxMinWinIntervalForProducers", 405},
	}
	for _, test := range cases {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			status, headers, body := request(t, api, test.method, test.path, "")
			if status != test.status {
				t.Fatalf("status=%d, want %d; body=%s", status, test.status, body)
			}
			var problem map[string]string
			if err := json.Unmarshal(body, &problem); err != nil || problem["error"] == "" {
				t.Fatalf("expected JSON error, got %s", body)
			}
			if status == 405 && headers.Get("Allow") != "GET, OPTIONS" {
				t.Fatalf("Allow=%q", headers.Get("Allow"))
			}
		})
	}
	t.Run("CORS", func(t *testing.T) {
		status, headers, _ := request(t, api, "GET", "/api/movies", "http://localhost:5173")
		if status != 200 || headers.Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
			t.Fatalf("allowed origin status=%d headers=%v", status, headers)
		}
		status, headers, _ = request(t, api, "OPTIONS", "/api/movies", "http://localhost:5173")
		if status != 204 || headers.Get("Access-Control-Allow-Methods") != "GET, OPTIONS" {
			t.Fatalf("preflight status=%d headers=%v", status, headers)
		}
		status, headers, _ = request(t, api, "GET", "/api/movies", "https://untrusted.example")
		if status != 200 || headers.Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("untrusted origin unexpectedly allowed: status=%d headers=%v", status, headers)
		}
		status, _, _ = request(t, api, "OPTIONS", "/api/movies", "https://untrusted.example")
		if status != 403 {
			t.Fatalf("untrusted preflight status=%d, want 403", status)
		}
	})
}
