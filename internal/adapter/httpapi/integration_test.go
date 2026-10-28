package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/alexspnovaes/outsera-backend/internal/adapter/csvfile"
	"github.com/alexspnovaes/outsera-backend/internal/adapter/httpapi"
	"github.com/alexspnovaes/outsera-backend/internal/adapter/sqlite"
	"github.com/alexspnovaes/outsera-backend/internal/application"
)

const csvHeader = "year;title;studios;producers;winner\n"

type prizeInterval struct {
	Producer     string `json:"producer"`
	Interval     int    `json:"interval"`
	PreviousWin  int    `json:"previousWin"`
	FollowingWin int    `json:"followingWin"`
}

type prizeIntervalResponse struct {
	Min []prizeInterval `json:"min"`
	Max []prizeInterval `json:"max"`
}

type runningAPI struct {
	server   *httptest.Server
	importer *application.Importer
	store    *sqlite.Store
	count    int
}

func startAPI(t *testing.T, source io.Reader) runningAPI {
	t.Helper()
	ctx := context.Background()
	store, err := sqlite.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	importer := application.NewImporter(csvfile.Reader{}, store)
	count, err := importer.Import(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.New(application.NewIntervals(store)))
	t.Cleanup(server.Close)
	return runningAPI{server: server, importer: importer, store: store, count: count}
}

func (a runningAPI) get(t *testing.T) (int, http.Header, []byte) {
	t.Helper()
	return a.request(t, http.MethodGet, httpapi.PrizeIntervalPath)
}

func (a runningAPI) request(t *testing.T, method, path string) (int, http.Header, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, a.server.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := a.server.Client().Do(req)
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

func decodeIntervals(t *testing.T, body []byte) prizeIntervalResponse {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var result prizeIntervalResponse
	if err := decoder.Decode(&result); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		t.Fatalf("response contains data after the JSON document: %s", body)
	}
	return result
}

func intervalsOf(t *testing.T, api runningAPI) prizeIntervalResponse {
	t.Helper()
	status, header, body := api.get(t)
	if status != http.StatusOK {
		t.Fatalf("status=%d body=%s", status, body)
	}
	if !strings.HasPrefix(header.Get("Content-Type"), "application/json") {
		t.Fatalf("Content-Type=%q", header.Get("Content-Type"))
	}
	if header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("X-Content-Type-Options=%q", header.Get("X-Content-Type-Options"))
	}
	return decodeIntervals(t, body)
}

func TestProvidedCSVMatchesProposal(t *testing.T) {
	file, err := os.Open("../../../data/Movielist.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	api := startAPI(t, file)
	if api.count != 206 {
		t.Fatalf("imported %d movies, want 206", api.count)
	}
	got := intervalsOf(t, api)
	want := prizeIntervalResponse{
		Min: []prizeInterval{{
			Producer: "Joel Silver", Interval: 1, PreviousWin: 1990, FollowingWin: 1991,
		}},
		Max: []prizeInterval{{
			Producer: "Matthew Vaughn", Interval: 13, PreviousWin: 2002, FollowingWin: 2015,
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("intervals=%+v, want %+v", got, want)
	}
}

func TestConsecutiveIntervalsReturnAllTies(t *testing.T) {
	source := csvHeader +
		"2008;A third;Studio Z;Alpha;yes\n" +
		"2000;A first;Studio Z;Alpha, Alpha;yes\n" +
		"2002;A second;Studio A;Alpha and Beta;yes\n" +
		"2004;B second;Studio A;Beta;yes\n" +
		"1995;G first;Studio B;Gamma;yes\n" +
		"2001;G second;Studio B;Gamma;yes\n" +
		"2003;Non winner;Unused;Only Nominee;\n"
	got := intervalsOf(t, startAPI(t, strings.NewReader(source)))
	want := prizeIntervalResponse{
		Min: []prizeInterval{
			{Producer: "Alpha", Interval: 2, PreviousWin: 2000, FollowingWin: 2002},
			{Producer: "Beta", Interval: 2, PreviousWin: 2002, FollowingWin: 2004},
		},
		Max: []prizeInterval{
			{Producer: "Alpha", Interval: 6, PreviousWin: 2002, FollowingWin: 2008},
			{Producer: "Gamma", Interval: 6, PreviousWin: 1995, FollowingWin: 2001},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("intervals=%+v, want %+v", got, want)
	}
}

func TestSameYearAwardsHaveZeroInterval(t *testing.T) {
	source := csvHeader +
		"2004;Later win;Studio;Alpha;yes\n" +
		"2000;First win;Studio;Alpha, Alpha;yes\n" +
		"2000;Another winning movie;Studio;Alpha;yes\n"
	got := intervalsOf(t, startAPI(t, strings.NewReader(source)))
	want := prizeIntervalResponse{
		Min: []prizeInterval{{Producer: "Alpha", Interval: 0, PreviousWin: 2000, FollowingWin: 2000}},
		Max: []prizeInterval{{Producer: "Alpha", Interval: 4, PreviousWin: 2000, FollowingWin: 2004}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("intervals=%+v, want %+v", got, want)
	}
}

func TestEmptyResultsAreArrays(t *testing.T) {
	sources := []string{
		csvHeader,
		csvHeader + "2000;Only award;Studio;Producer;yes\n",
	}
	for _, source := range sources {
		api := startAPI(t, strings.NewReader(source))
		status, _, body := api.get(t)
		if status != http.StatusOK {
			t.Fatalf("status=%d body=%s", status, body)
		}
		if bytes.Contains(body, []byte(`null`)) {
			t.Fatalf("empty result must be arrays, got %s", body)
		}
		got := decodeIntervals(t, body)
		if len(got.Min) != 0 || len(got.Max) != 0 {
			t.Fatalf("intervals=%+v", got)
		}
	}
}

func TestInvalidImportPreservesPreviousDataset(t *testing.T) {
	api := startAPI(t, strings.NewReader(csvHeader+
		"2000;First;Studio;Ada;yes\n"+
		"2007;Second;Studio;Ada;yes\n"))
	original := prizeIntervalResponse{
		Min: []prizeInterval{{Producer: "Ada", Interval: 7, PreviousWin: 2000, FollowingWin: 2007}},
		Max: []prizeInterval{{Producer: "Ada", Interval: 7, PreviousWin: 2000, FollowingWin: 2007}},
	}
	if got := intervalsOf(t, api); !reflect.DeepEqual(got, original) {
		t.Fatalf("initial intervals=%+v", got)
	}

	cases := []struct {
		name   string
		source string
	}{
		{name: "empty file", source: ""},
		{name: "wrong header", source: "year;name;studios;producers;winner\n"},
		{name: "invalid year", source: csvHeader + "not-a-year;Invalid;Studio;Producer;yes\n"},
		{name: "year out of range", source: csvHeader + "0;Invalid;Studio;Producer;yes\n"},
		{name: "missing column", source: csvHeader + "2000;Invalid;Studio;Producer\n"},
		{name: "empty title", source: csvHeader + "2000;;Studio;Producer;yes\n"},
		{name: "empty studio", source: csvHeader + "2000;Invalid;;Producer;yes\n"},
		{name: "empty producer", source: csvHeader + "2000;Invalid;Studio;;yes\n"},
		{name: "invalid winner", source: csvHeader + "2000;Invalid;Studio;Producer;maybe\n"},
		{name: "malformed quoting", source: csvHeader + "2000;\"Invalid;Studio;Producer;yes\n"},
		{name: "partial file", source: csvHeader + "2001;Would replace;Studio;Other;yes\ninvalid;Bad;Studio;Other;yes\n"},
		{name: "file too large", source: oversizedCSV()},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, err := api.importer.Import(context.Background(), strings.NewReader(test.source))
			if err == nil {
				t.Fatal("invalid import unexpectedly succeeded")
			}
			if got := intervalsOf(t, api); !reflect.DeepEqual(got, original) {
				t.Fatalf("failed import changed stored intervals: %+v", got)
			}
		})
	}

	t.Run("cancelled write keeps previous data", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		replacement := csvHeader + "2010;Replacement;Studio;Zoe;yes\n2011;Replacement 2;Studio;Zoe;yes\n"
		if _, err := api.importer.Import(ctx, strings.NewReader(replacement)); err == nil {
			t.Fatal("cancelled import unexpectedly succeeded")
		}
		if got := intervalsOf(t, api); !reflect.DeepEqual(got, original) {
			t.Fatalf("cancelled import changed stored intervals: %+v", got)
		}
	})

	t.Run("valid replacement", func(t *testing.T) {
		source := "\ufeff\"year\";\"title\";\"studios\";\"producers\";\"winner\"\n" +
			"2000;A;Studio and Partners;Alice, Bob, and Charlie;yes\n" +
			"2002;B;Other;Alice;yes\n" +
			"2005;C;Other;Charlie;yes\n"
		count, err := api.importer.Import(context.Background(), strings.NewReader(source))
		if err != nil || count != 3 {
			t.Fatalf("count=%d err=%v", count, err)
		}
		want := prizeIntervalResponse{
			Min: []prizeInterval{{Producer: "Alice", Interval: 2, PreviousWin: 2000, FollowingWin: 2002}},
			Max: []prizeInterval{{Producer: "Charlie", Interval: 5, PreviousWin: 2000, FollowingWin: 2005}},
		}
		if got := intervalsOf(t, api); !reflect.DeepEqual(got, want) {
			t.Fatalf("intervals=%+v, want %+v", got, want)
		}
	})
}

func TestInternalErrorIsNotExposed(t *testing.T) {
	api := startAPI(t, strings.NewReader(csvHeader+"2000;Movie;Studio;Producer;yes\n"))
	if err := api.store.Close(); err != nil {
		t.Fatal(err)
	}

	status, header, body := api.get(t)
	if status != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", status, body)
	}
	if header.Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type=%q", header.Get("Content-Type"))
	}
	if header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("X-Content-Type-Options=%q", header.Get("X-Content-Type-Options"))
	}
	const want = "{\"error\":\"internal server error\"}\n"
	if string(body) != want {
		t.Fatalf("body=%q, want %q", body, want)
	}
}

func oversizedCSV() string {
	return csvHeader + "2000;" + strings.Repeat("x", int(csvfile.MaxFileSize)) + ";Studio;Producer;yes\n"
}

func TestHTTPStatusCodes(t *testing.T) {
	api := startAPI(t, strings.NewReader(csvHeader+"2000;Movie;Studio;Producer;yes\n"))

	t.Run("head has no body", func(t *testing.T) {
		status, header, body := api.request(t, http.MethodHead, httpapi.PrizeIntervalPath)
		if status != http.StatusOK {
			t.Fatalf("status=%d", status)
		}
		if len(body) != 0 {
			t.Fatalf("HEAD body=%q", body)
		}
		if !strings.HasPrefix(header.Get("Content-Type"), "application/json") {
			t.Fatalf("Content-Type=%q", header.Get("Content-Type"))
		}
	})

	cases := []struct {
		method string
		path   string
		status int
	}{
		{http.MethodPost, httpapi.PrizeIntervalPath, http.StatusMethodNotAllowed},
		{http.MethodPut, httpapi.PrizeIntervalPath, http.StatusMethodNotAllowed},
		{http.MethodDelete, httpapi.PrizeIntervalPath, http.StatusMethodNotAllowed},
		{http.MethodGet, "/api/movies", http.StatusNotFound},
		{http.MethodGet, httpapi.PrizeIntervalPath + "/", http.StatusNotFound},
		{http.MethodGet, "/health", http.StatusNotFound},
	}
	for _, test := range cases {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			status, header, body := api.request(t, test.method, test.path)
			if status != test.status {
				t.Fatalf("status=%d, want %d; body=%s", status, test.status, body)
			}
			var problem map[string]string
			if err := json.Unmarshal(body, &problem); err != nil || problem["error"] == "" {
				t.Fatalf("expected JSON error, got %s", body)
			}
			if status == http.StatusMethodNotAllowed && header.Get("Allow") != "GET, HEAD" {
				t.Fatalf("Allow=%q", header.Get("Allow"))
			}
		})
	}
}
