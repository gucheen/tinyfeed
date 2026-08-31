package tinyfeed

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"
)

// implement flag.Value
// Adapted from https://stackoverflow.com/a/28323276
type StringRepeatable []string

func (i *StringRepeatable) String() string {
	return fmt.Sprintf("%v", *i)
}

func (i *StringRepeatable) Set(value string) error {
	*i = append(*i, value)
	return nil
}

// flags
type Config struct {
	Urls             []string
	FeedFilters      map[string]ItemFilter
	Limit            int
	LimitPerFeed     int
	Timeout          int
	RequestSemaphore int
	Name             string
	Description      string
	Quiet            bool
	Stylesheets      StringRepeatable
	Scripts          StringRepeatable
	TemplatePath     string
	Input            string
	Output           string
	Daemon           bool
	Interval         int64
	OrderBy          string
	// The field bellow are only used during testing to ensure a stable ouput
	Nonce     string
	UpdatedAt time.Time
}

var DefaultConfig = Config{
	Limit:            256,
	LimitPerFeed:     256,
	Timeout:          15,
	RequestSemaphore: 16,
	Name:             "Feed",
	Interval:         1440,
	OrderBy:          "publication-date",
}

func init() {
	log.SetOutput(os.Stderr)
	log.SetFlags(0)
}

func printHelp() {
	d := DefaultConfig
	log.Printf(`Aggregate a collection of feeds into static HTML page

Usage:
  tinyfeed [flags] [FEED_URL ...]

Examples:
  single feed      tinyfeed https://feed.lovergne.dev/releases.atom > index.html
  multiple feeds   cat feeds.txt | tinyfeed > index.html
  daemon mode      tinyfeed --daemon -i feeds.txt -o index.html

Feed filters:
  Append include-url-pattern=REGEXP or exclude-url-pattern=REGEXP after a feed URL.
  tinyfeed https://example.com/feed.xml 'include-url-pattern=/en/'

Flags:

  Main flags:
  -i, --input string         Path to a file with a list of feeds.
  -o, --output string        Path to a file to save the output to.
  -D, --daemon               Whether to execute the program in a daemon mode.
  
  Customization flags:
  -n, --name string          Title of the page. (default "%s")
  -d, --description string   Add a description after the name of your page
  -s, --stylesheet string    Link to an external CSS stylesheet
  -S, --script string        Link to an external JavaScript file
  -t, --template string      Path to a custom HTML+Go template file.

  Configuration flags:
  -I, --interval int         Duration in minutes between execution. Ignored if not in daemon mode. (default %d)
  -l, --limit int            How many articles to display in total (default %d)
  -L, --limit-per-feed int   Maximum number of articles to display per feed (default %d)
  -q, --quiet                Add this flag to silence warnings.
  -r, --requests int         How many simultaneous requests can be made (default %d)
  -T, --timeout int          Timeout to get feeds in seconds (default %d)
  -O, --order-by string      How to order the articles. Accept 'publication-date', 'update-date', 'feed-name','author'. (default %s)

  -h, --help                 help for tinyfeed

For the full tinyfeed manual, please visit: https://feed.lovergne.dev/`,
		d.Name, d.Interval, d.Limit, d.LimitPerFeed, d.RequestSemaphore, d.Timeout, d.OrderBy,
	)
	os.Exit(0)
}

func parseCmd(args []string, stdin io.Reader) (*Config, error) {
	config := DefaultConfig
	config.Scripts = make(StringRepeatable, 0)
	config.Stylesheets = make(StringRepeatable, 0)
	config.FeedFilters = make(map[string]ItemFilter)

	fs := flag.NewFlagSet("tinyfeed", flag.ContinueOnError)
	fs.Usage = func() {} // Disable default usage message

	fs.IntVar(&config.Limit, "limit", DefaultConfig.Limit, "How many articles to display in total")
	fs.IntVar(&config.Limit, "l", DefaultConfig.Limit, "How many articles to display in total")

	fs.IntVar(&config.LimitPerFeed, "limit-per-feed", DefaultConfig.LimitPerFeed, "Maximum number of articles to display per feed")
	fs.IntVar(&config.LimitPerFeed, "L", DefaultConfig.LimitPerFeed, "Maximum number of articles to display per feed")

	fs.IntVar(&config.RequestSemaphore, "requests", DefaultConfig.RequestSemaphore, "How many simulaneous requests can be made")
	fs.IntVar(&config.RequestSemaphore, "r", DefaultConfig.RequestSemaphore, "How many simulaneous requests can be made")

	fs.IntVar(&config.Timeout, "timeout", DefaultConfig.Timeout, "Timeout to get feeds in seconds")
	fs.IntVar(&config.Timeout, "T", DefaultConfig.Timeout, "Timeout to get feeds in seconds")

	fs.StringVar(&config.Name, "name", DefaultConfig.Name, "Title of the page")
	fs.StringVar(&config.Name, "n", DefaultConfig.Name, "Title of the page")

	fs.StringVar(&config.Description, "description", DefaultConfig.Description, "Add a description after the name of your page")
	fs.StringVar(&config.Description, "d", DefaultConfig.Description, "Add a description after the name of your page")

	fs.BoolVar(&config.Quiet, "quiet", DefaultConfig.Quiet, "Silence warnings")
	fs.BoolVar(&config.Quiet, "q", DefaultConfig.Quiet, "Silence warnings")

	fs.Var(&config.Stylesheets, "stylesheet", "Link to an external CSS stylesheet")
	fs.Var(&config.Stylesheets, "s", "Link to an external CSS stylesheet")

	fs.Var(&config.Scripts, "script", "Link to an external JavaScript file")
	fs.Var(&config.Scripts, "S", "Link to an external JavaScript file")

	fs.StringVar(&config.TemplatePath, "template", DefaultConfig.TemplatePath, "Path to a custom HTML+Go template file")
	fs.StringVar(&config.TemplatePath, "t", DefaultConfig.TemplatePath, "Path to a custom HTML+Go template file")

	fs.StringVar(&config.Input, "input", DefaultConfig.Input, "Path to a file with a list of feeds")
	fs.StringVar(&config.Input, "i", DefaultConfig.Input, "Path to a file with a list of feeds")

	fs.StringVar(&config.Output, "output", DefaultConfig.Output, "Path to a file to save the output to")
	fs.StringVar(&config.Output, "o", DefaultConfig.Output, "Path to a file to save the output to")

	fs.BoolVar(&config.Daemon, "daemon", DefaultConfig.Daemon, "Whether to execute the program in a daemon mode")
	fs.BoolVar(&config.Daemon, "D", DefaultConfig.Daemon, "Whether to execute the program in a daemon mode")

	fs.Int64Var(&config.Interval, "interval", DefaultConfig.Interval, "Duration in minutes between execution. Ignored if not in daemon mode")
	fs.Int64Var(&config.Interval, "I", DefaultConfig.Interval, "Duration in minutes between execution. Ignored if not in daemon mode")

	fs.StringVar(&config.OrderBy, "order-by", DefaultConfig.OrderBy, "How to order the articles (publication-date, update-date, feed-name, author)")
	fs.StringVar(&config.OrderBy, "O", DefaultConfig.OrderBy, "How to order the articles (publication-date, update-date, feed-name, author)")

	urls, err := parseFlagsToTheEnd(fs, args)
	if err != nil {
		return nil, fmt.Errorf("failed to parse flags: %w", err)
	}

	argumentFeeds, err := parseFeedSources(urls)
	if err != nil {
		return nil, fmt.Errorf("failed to parse feed arguments: %w", err)
	}

	// Read stdin once so the daemon can reuse its feed configuration.
	var stdinFeeds []FeedSource
	if stdin != nil {
		stdinFeeds, err = readerToFeedSources(stdin)
		if err != nil {
			return nil, fmt.Errorf("failed to parse stdin: %w", err)
		}
	}
	feeds := mergeFeedSources(argumentFeeds, stdinFeeds)
	config.Urls, config.FeedFilters = splitFeedSources(feeds)

	err = validateOrderBy(config.OrderBy)
	if err != nil {
		return nil, err
	}

	return &config, nil
}

// This function allow to parse the flags that are passed after aguments in order to
// avoid breaking after the replacement of cobra by the std flag package.
// Basicly the following flag is parsed correclty using cobra
//
//	tinyfeed https://lovergne.dev/rss.xml -o index.html
//
// But not using the std flag package.
// Adapted from https://github.com/golang/go/issues/36744 by thinkerbot
func parseFlagsToTheEnd(fs *flag.FlagSet, args []string) ([]string, error) {
	err := fs.Parse(args)
	if err != nil {
		return nil, err
	}
	positionals := make([]string, 0)
	for i := len(args) - len(fs.Args()) + 1; i < len(args); {
		if i > 1 && args[i-2] == "--" {
			break
		}
		positionals = append(positionals, fs.Arg(0))
		err = fs.Parse(args[i:])
		if err != nil {
			return nil, err
		}
		i += 1 + len(args[i:]) - len(fs.Args())
	}
	return append(positionals, fs.Args()...), nil
}

// Return os.Stdin if something is piped to the program, nil otherwise
func pipedStdin() io.Reader {
	fi, err := os.Stdin.Stat()
	if err != nil || (fi.Mode()&os.ModeCharDevice) != 0 {
		return nil
	}
	return os.Stdin
}

func fileToArgs(filepath string) ([]string, error) {
	if filepath == "" {
		return []string{}, nil
	}
	file, err := os.Open(filepath)
	if err != nil {
		return nil, fmt.Errorf("error opening input file: %s", err)
	}
	return readerToArgs(file)
}

func fileToFeedSources(filepath string) ([]FeedSource, error) {
	if filepath == "" {
		return []FeedSource{}, nil
	}
	file, err := os.Open(filepath)
	if err != nil {
		return nil, fmt.Errorf("error opening input file: %s", err)
	}
	defer file.Close()
	return readerToFeedSources(file)
}

func readerToArgs(reader io.Reader) ([]string, error) {
	input, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("error reading input: %s", err)
	}
	lines := strings.Split(string(input), "\n")

	argsSet := make(map[string]struct{})
	for _, s := range lines {
		lineTrimmed := strings.TrimSpace(s)
		if strings.HasPrefix(lineTrimmed, "#") || lineTrimmed == "" {
			continue
		}
		for _, field := range strings.Fields(lineTrimmed) {
			argsSet[field] = struct{}{}
		}
	}

	i := 0
	args := make([]string, len(argsSet))
	for k := range argsSet {
		args[i] = k
		i++
	}
	return args, nil
}

func readerToFeedSources(reader io.Reader) ([]FeedSource, error) {
	input, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("error reading input: %s", err)
	}

	var feeds []FeedSource
	for lineNumber, line := range strings.Split(string(input), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lineFeeds, err := parseFeedSources(strings.Fields(line))
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNumber+1, err)
		}
		feeds = mergeFeedSources(feeds, lineFeeds)
	}
	return feeds, nil
}

func parseFeedSources(args []string) ([]FeedSource, error) {
	var feeds []FeedSource
	indexes := make(map[string]int)
	current := -1

	for _, arg := range args {
		name, pattern, isFilter := strings.Cut(arg, "=")
		isFilterName := name == "include-url-pattern" || name == "exclude-url-pattern"
		if isFilterName && !isFilter {
			return nil, fmt.Errorf("%s requires a pattern after =", name)
		}
		if isFilterName {
			if current == -1 {
				return nil, fmt.Errorf("%s must follow a feed URL", name)
			}
			if pattern == "" {
				return nil, fmt.Errorf("%s cannot be empty", name)
			}
			filter := &feeds[current].Filter
			if name == "include-url-pattern" {
				filter.IncludeURLPatterns = append(filter.IncludeURLPatterns, pattern)
			} else {
				filter.ExcludeURLPatterns = append(filter.ExcludeURLPatterns, pattern)
			}
			continue
		}

		if index, ok := indexes[arg]; ok {
			current = index
			continue
		}
		indexes[arg] = len(feeds)
		feeds = append(feeds, FeedSource{URL: arg})
		current = len(feeds) - 1
	}

	return feeds, nil
}

func mergeFeedSources(groups ...[]FeedSource) []FeedSource {
	var merged []FeedSource
	indexes := make(map[string]int)
	for _, feeds := range groups {
		for _, feed := range feeds {
			index, ok := indexes[feed.URL]
			if !ok {
				indexes[feed.URL] = len(merged)
				merged = append(merged, feed)
				continue
			}
			merged[index].Filter.IncludeURLPatterns = append(
				merged[index].Filter.IncludeURLPatterns,
				feed.Filter.IncludeURLPatterns...,
			)
			merged[index].Filter.ExcludeURLPatterns = append(
				merged[index].Filter.ExcludeURLPatterns,
				feed.Filter.ExcludeURLPatterns...,
			)
		}
	}
	return merged
}

func splitFeedSources(feeds []FeedSource) ([]string, map[string]ItemFilter) {
	urls := make([]string, 0, len(feeds))
	filters := make(map[string]ItemFilter)
	for _, feed := range feeds {
		urls = append(urls, feed.URL)
		if len(feed.Filter.IncludeURLPatterns) > 0 || len(feed.Filter.ExcludeURLPatterns) > 0 {
			filters[feed.URL] = feed.Filter
		}
	}
	return urls, filters
}

func validateOrderBy(orderBy string) error {
	accepted := []string{"publication-date", "update-date", "feed-name", "author"}
	for _, accept := range accepted {
		if orderBy == accept {
			return nil
		}
	}
	return fmt.Errorf(
		"Invalid value '%s' for flag --order-by, accepted values are %s.",
		orderBy,
		strings.Join(accepted, ", "),
	)
}
