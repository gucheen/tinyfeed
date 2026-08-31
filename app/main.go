package tinyfeed

import (
	_ "embed"
	"flag"
	"fmt"
	"html"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/mmcdole/gofeed"
)

//go:embed templates/index.html.gtpl
var builtInTemplate string

//go:embed templates/index.opml.gtpl
var opmlTemplate string

type Item struct {
	*gofeed.Item
	FeedName string
}

type ItemFilter struct {
	IncludeURLPatterns []string
	ExcludeURLPatterns []string
}

type FeedSource struct {
	URL    string
	Filter ItemFilter
}

type compiledItemFilter struct {
	include []*regexp.Regexp
	exclude []*regexp.Regexp
}

type compiledFeedSource struct {
	URL    string
	Filter compiledItemFilter
}

func Main() {
	config, err := parseCmd()
	if err != nil {
		if err == flag.ErrHelp {
			printHelp()
			os.Exit(0)
		}
		log.Printf("%s\n", err)
		os.Exit(1)
	}

	err = Run(config)
	if err != nil {
		log.Printf("%s\n", err)
		os.Exit(1)
	}

	if !config.Daemon {
		return
	}

	// Daemon mode
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)
	ticker := time.NewTicker(time.Minute * time.Duration(config.Interval))

	for {
		select {
		case <-signalChan:
			return
		case <-ticker.C:
			err = Run(config)
			if err != nil {
				log.Printf("%s\n", err)
				os.Exit(1)
			}
		}
	}
}

func Run(config *Config) error {
	// We append inputs from file here so that it can be updated without reloading the daemon
	fileFeeds, err := fileToFeedSources(config.Input)
	if err != nil {
		return fmt.Errorf("fail to parse input file: %w", err)
	}
	configuredFeeds := make([]FeedSource, 0, len(config.Urls))
	for _, url := range config.Urls {
		configuredFeeds = append(configuredFeeds, FeedSource{
			URL:    url,
			Filter: config.FeedFilters[url],
		})
	}
	feedsToParse := mergeFeedSources(configuredFeeds, fileFeeds)

	if len(feedsToParse) == 0 {
		return fmt.Errorf(
			"no argument found, you must input at least one feed url. Use `tinyfeed --help` for examples",
		)
	}

	feeds, err := parseFeeds(feedsToParse, config)
	if err != nil {
		return fmt.Errorf("fail to configure feed filters: %w", err)
	}
	items := prepareItems(feeds, config)

	err = printHTML(feeds, items, config)
	if err != nil {
		return fmt.Errorf("fail to output HTML: %w", err)
	}
	return nil
}

func parseFeeds(sources []FeedSource, config *Config) ([]*gofeed.Feed, error) {
	compiledSources, err := compileFeedSources(sources)
	if err != nil {
		return nil, err
	}

	var sem = make(chan struct{}, config.RequestSemaphore)
	var results = make(chan *gofeed.Feed)
	var wg sync.WaitGroup
	wg.Add(len(compiledSources))

	fp := gofeed.NewParser()
	fp.UserAgent = "tinyfeed/v1"
	fp.Client = &http.Client{Timeout: time.Duration(config.Timeout * int(time.Second))}

	for _, source := range compiledSources {
		go func(source compiledFeedSource) {
			defer func() {
				wg.Done()
				<-sem
			}()
			sem <- struct{}{}
			results <- parseFeed(source, fp, config)
		}(source)
	}

	go func() {
		wg.Wait()
		close(sem)
		close(results)
	}()

	feeds := []*gofeed.Feed{}
	for feed := range results {
		if feed != nil {
			feeds = append(feeds, feed)
		}
	}
	return feeds, nil
}

func parseFeed(source compiledFeedSource, fp *gofeed.Parser, config *Config) *gofeed.Feed {
	feed, err := fp.ParseURL(source.URL)
	if err != nil {
		if !config.Quiet {
			log.Printf("WARNING: fail to process feed at %s: %s\n", source.URL, err)
		}
		return nil
	}

	if feed.FeedLink == "" {
		feed.FeedLink = source.URL
	}

	feed.Items = filterItemsByURL(feed.Items, source.Filter)
	feed.Items = feed.Items[:min(len(feed.Items), config.LimitPerFeed)]

	return feed
}

func compileFeedSources(sources []FeedSource) ([]compiledFeedSource, error) {
	compiled := make([]compiledFeedSource, 0, len(sources))
	for _, source := range sources {
		filter, err := compileItemFilter(source.Filter)
		if err != nil {
			return nil, fmt.Errorf("feed %s: %w", source.URL, err)
		}
		compiled = append(compiled, compiledFeedSource{URL: source.URL, Filter: filter})
	}
	return compiled, nil
}

func compileItemFilter(filter ItemFilter) (compiledItemFilter, error) {
	compiled := compiledItemFilter{}
	for _, pattern := range filter.IncludeURLPatterns {
		expression, err := regexp.Compile(pattern)
		if err != nil {
			return compiledItemFilter{}, fmt.Errorf("invalid include URL pattern %q: %w", pattern, err)
		}
		compiled.include = append(compiled.include, expression)
	}
	for _, pattern := range filter.ExcludeURLPatterns {
		expression, err := regexp.Compile(pattern)
		if err != nil {
			return compiledItemFilter{}, fmt.Errorf("invalid exclude URL pattern %q: %w", pattern, err)
		}
		compiled.exclude = append(compiled.exclude, expression)
	}
	return compiled, nil
}

func filterItemsByURL(items []*gofeed.Item, filter compiledItemFilter) []*gofeed.Item {
	filtered := make([]*gofeed.Item, 0, len(items))
	for _, item := range items {
		if matchesURLFilter(item.Link, filter) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func matchesURLFilter(url string, filter compiledItemFilter) bool {
	for _, pattern := range filter.exclude {
		if pattern.MatchString(url) {
			return false
		}
	}
	if len(filter.include) == 0 {
		return true
	}
	for _, pattern := range filter.include {
		if pattern.MatchString(url) {
			return true
		}
	}
	return false
}

func prepareItems(feeds []*gofeed.Feed, config *Config) []Item {
	var items []Item

	for _, feed := range feeds {
		for _, item := range feed.Items {
			items = append(items, Item{
				item,
				feed.Title,
			})
		}
	}

	for i := range items {
		if items[i].Title == "" {
			items[i].Title = "Untitled"
		}

		// Some string are already html escaped inside the feeds and when
		// html/template Run it re-escape them, creating double escape. In
		// order to avoid malformed string we must unescape first.
		items[i].Title = html.UnescapeString(items[i].Title)
		items[i].Link = html.UnescapeString(items[i].Link)
		items[i].Description = html.UnescapeString(items[i].Description)
		items[i].Content = html.UnescapeString(items[i].Content)
		items[i].Published = html.UnescapeString(items[i].Published)
		items[i].FeedName = html.UnescapeString(items[i].FeedName)
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].PublishedParsed == nil {
			return false
		}
		if items[j].PublishedParsed == nil {
			return true
		}
		return items[i].PublishedParsed.After(*items[j].PublishedParsed)
	})

	items = sortItems(items, config.OrderBy)

	return items[0:min(len(items), config.Limit)]
}

func printHTML(feeds []*gofeed.Feed, items []Item, config *Config) error {
	var err error
	var ts *template.Template
	var templateFuncs = template.FuncMap{"domain": domain, "publication": publication, "opmlRSSVersion": opmlRSSVersion}
	switch config.TemplatePath {
	case "":
		ts, err = template.New("templates/index.html.gtpl").
			Funcs(templateFuncs).
			Parse(builtInTemplate)
	case "opml":
		ts, err = template.New("templates/index.opml.gtpl").
			Funcs(templateFuncs).
			Parse(opmlTemplate)
	default:
		ts, err = template.New(filepath.Base(config.TemplatePath)).
			Funcs(templateFuncs).
			ParseFiles(config.TemplatePath)
	}

	if err != nil {
		return fmt.Errorf("fail to load HTML template: %w", err)
	}
	currDate := time.Now()

	data := struct {
		// Metadata is an arbitrary map, there is no backward compatibility promise on it's content.
		// Changing it's content won't be concidered breaking change. (But we keep it as stable as possible)
		// In the future, if we are confident that it won't evolve, we will replace it with a struct
		// to ensure backward compatibiliy
		Metadata    map[string]string
		Items       []Item
		Feeds       []*gofeed.Feed
		Stylesheets []string
		Scripts     []string
	}{
		Metadata: map[string]string{
			"name":            config.Name,
			"description":     config.Description,
			"nonce":           generateNonce(256),
			"day":             currDate.Weekday().String(),
			"datetime":        currDate.Format(time.DateTime),
			"datetimeRFC3339": currDate.Format(time.RFC3339),
			"datetimeRFC822":  currDate.Format(time.RFC822),
		},
		Items:       items,
		Feeds:       feeds,
		Stylesheets: config.Stylesheets,
		Scripts:     config.Scripts,
	}

	var outFile io.WriteCloser
	if config.Output != "" {
		outFile, err = os.Create(config.Output)
		if err != nil {
			return err
		}
		defer outFile.Close()
	} else {
		outFile = os.Stdout
	}

	err = ts.Execute(outFile, data)
	if err != nil {
		return fmt.Errorf("fail to render HTML template: %w", err)
	}

	return nil
}
