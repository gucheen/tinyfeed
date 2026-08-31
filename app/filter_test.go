package tinyfeed

import (
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/mmcdole/gofeed"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestReaderToFeedSources(t *testing.T) {
	input := strings.NewReader(`# multilingual feed
https://example.com/feed.xml include-url-pattern=/en/ include-url-pattern=/news/ exclude-url-pattern=/sponsored/
https://plain.example/feed.xml
`)

	got, err := readerToFeedSources(input)
	if err != nil {
		t.Fatalf("readerToFeedSources() error = %v", err)
	}
	want := []FeedSource{
		{
			URL: "https://example.com/feed.xml",
			Filter: ItemFilter{
				IncludeURLPatterns: []string{"/en/", "/news/"},
				ExcludeURLPatterns: []string{"/sponsored/"},
			},
		},
		{URL: "https://plain.example/feed.xml"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("readerToFeedSources() got = %#v, want %#v", got, want)
	}
}

func TestParseFeedSourcesRejectsFilterWithoutFeed(t *testing.T) {
	_, err := parseFeedSources([]string{"include-url-pattern=/en/"})
	if err == nil {
		t.Fatal("parseFeedSources() expected an error")
	}
}

func TestParseFeedSourcesRejectsFilterWithoutPattern(t *testing.T) {
	_, err := parseFeedSources([]string{"https://example.com/feed.xml", "include-url-pattern"})
	if err == nil {
		t.Fatal("parseFeedSources() expected an error")
	}
}

func TestMergeFeedSourcesCombinesRulesForDuplicateFeed(t *testing.T) {
	first := []FeedSource{{
		URL: "https://example.com/feed.xml",
		Filter: ItemFilter{
			IncludeURLPatterns: []string{"/en/"},
		},
	}}
	second := []FeedSource{{
		URL: "https://example.com/feed.xml",
		Filter: ItemFilter{
			ExcludeURLPatterns: []string{"/sponsored/"},
		},
	}}

	got := mergeFeedSources(first, second)
	want := []FeedSource{{
		URL: "https://example.com/feed.xml",
		Filter: ItemFilter{
			IncludeURLPatterns: []string{"/en/"},
			ExcludeURLPatterns: []string{"/sponsored/"},
		},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("mergeFeedSources() got = %#v, want %#v", got, want)
	}
}

func TestFilterItemsByURL(t *testing.T) {
	filter, err := compileItemFilter(ItemFilter{
		IncludeURLPatterns: []string{`/en/`, `/news/`},
		ExcludeURLPatterns: []string{`/sponsored/`},
	})
	if err != nil {
		t.Fatalf("compileItemFilter() error = %v", err)
	}
	items := []*gofeed.Item{
		{Link: "https://example.com/en/first"},
		{Link: "https://example.com/fr/second"},
		{Link: "https://example.com/news/third"},
		{Link: "https://example.com/en/sponsored/fourth"},
	}

	got := filterItemsByURL(items, filter)
	want := []string{
		"https://example.com/en/first",
		"https://example.com/news/third",
	}
	if len(got) != len(want) {
		t.Fatalf("filterItemsByURL() returned %d items, want %d", len(got), len(want))
	}
	for index := range got {
		if got[index].Link != want[index] {
			t.Errorf("filterItemsByURL()[%d] = %q, want %q", index, got[index].Link, want[index])
		}
	}
}

func TestFilterItemsByURLWithoutIncludeIsNotAWhitelist(t *testing.T) {
	filter, err := compileItemFilter(ItemFilter{
		ExcludeURLPatterns: []string{`/draft/`},
	})
	if err != nil {
		t.Fatalf("compileItemFilter() error = %v", err)
	}
	items := []*gofeed.Item{
		{Link: "https://example.com/published/first"},
		{Link: "https://example.com/draft/second"},
	}

	got := filterItemsByURL(items, filter)
	if len(got) != 1 || got[0].Link != items[0].Link {
		t.Errorf("filterItemsByURL() got = %#v, want only the published item", got)
	}
}

func TestCompileItemFilterRejectsInvalidPattern(t *testing.T) {
	_, err := compileItemFilter(ItemFilter{IncludeURLPatterns: []string{"["}})
	if err == nil {
		t.Fatal("compileItemFilter() expected an error")
	}
}

func TestParseFeedFiltersBeforeApplyingPerFeedLimit(t *testing.T) {
	feedDocument := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Multilingual feed</title>
    <link>https://example.com</link>
    <description>Test feed</description>
    <item><title>French</title><link>https://example.com/fr/first</link></item>
    <item><title>English</title><link>https://example.com/en/second</link></item>
    <item><title>English 2</title><link>https://example.com/en/third</link></item>
  </channel>
</rss>`

	filter, err := compileItemFilter(ItemFilter{IncludeURLPatterns: []string{`/en/`}})
	if err != nil {
		t.Fatalf("compileItemFilter() error = %v", err)
	}
	parser := gofeed.NewParser()
	parser.Client = &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/rss+xml"}},
			Body:       io.NopCloser(strings.NewReader(feedDocument)),
		}, nil
	})}
	feed := parseFeed(
		compiledFeedSource{URL: "https://feed.example/feed.xml", Filter: filter},
		parser,
		&Config{LimitPerFeed: 1, Timeout: 5, Quiet: true},
	)

	if feed == nil {
		t.Fatal("parseFeed() returned nil")
	}
	if len(feed.Items) != 1 || feed.Items[0].Link != "https://example.com/en/second" {
		t.Errorf("parseFeed() items = %#v, want the first matching English item", feed.Items)
	}
}
