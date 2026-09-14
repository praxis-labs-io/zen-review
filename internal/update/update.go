// Package update checks for a newer release and runs the published installer.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	latestReleaseURL = "https://api.github.com/repos/praxis-labs-io/zen-review/releases/latest"

	cacheTTL = 24 * time.Hour

	requestTimeout = 5 * time.Second

	devVersion = "dev"

	maxBodyBytes = 1 << 20
)

// Result's Latest is empty when the check had no answer.
type Result struct {
	Latest    string
	Available bool
}

type Options struct {
	Current string
	// CachePath empty skips the cache in both directions.
	CachePath string

	endpoint string
	now      func() time.Time
}

// Check reports whether a release newer than Current is published, answering from a fresh cache.
// A dev Current is never checked, and a failed cache write returns the result with its error.
func Check(ctx context.Context, opts Options) (Result, error) {
	if opts.Current == "" || opts.Current == devVersion {
		return Result{}, nil
	}

	now := time.Now
	if opts.now != nil {
		now = opts.now
	}

	if cached := loadCache(opts.CachePath); cached.fresh(now(), cacheTTL) {
		return resultFor(opts.Current, cached.LatestTag), nil
	}

	tag, err := fetchLatestTag(ctx, opts)
	if err != nil {
		return Result{}, err
	}

	if _, ok := parseVersion(tag); !ok {
		return Result{}, fmt.Errorf("unrecognized release tag %q", tag)
	}

	if opts.CachePath != "" {
		if err := recordCache(opts.CachePath, tag, now()); err != nil {
			return resultFor(opts.Current, tag), err
		}
	}

	return resultFor(opts.Current, tag), nil
}

func resultFor(current, tag string) Result {
	return Result{Latest: tag, Available: isNewer(current, tag)}
}

func fetchLatestTag(ctx context.Context, opts Options) (string, error) {
	endpoint := opts.endpoint
	if endpoint == "" {
		endpoint = latestReleaseURL
	}
	client := &http.Client{Timeout: requestTimeout}

	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("building the release request: %w", err)
	}
	req.Header.Set("User-Agent", "zen-review/"+opts.Current)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("requesting the latest release: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("the latest release lookup answered %s", resp.Status)
	}

	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBodyBytes)).Decode(&payload); err != nil {
		return "", fmt.Errorf("parsing the latest release: %w", err)
	}
	if payload.TagName == "" {
		return "", fmt.Errorf("the latest release named no tag")
	}

	return payload.TagName, nil
}
