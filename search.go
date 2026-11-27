package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

type searchMatch struct {
	Item       item
	SourceID   string
	SourceName string
}

func exactTitleMatches(matches []searchMatch, query string) []searchMatch {
	var exact []searchMatch
	for _, match := range matches {
		if strings.EqualFold(strings.TrimSpace(match.Item.Title), query) {
			exact = append(exact, match)
		}
	}
	return exact
}

func searchSources(ctx context.Context, client *http.Client, sources []source, query string) ([]searchMatch, []error) {
	type batch struct {
		items []item
		err   error
	}
	batches := make([]batch, len(sources))
	jobs := make(chan int, len(sources))
	for i := range sources {
		jobs <- i
	}
	close(jobs)

	var workers sync.WaitGroup
	for range min(6, len(sources)) {
		workers.Go(func() {
			for i := range jobs {
				if ctx.Err() != nil {
					return
				}
				batches[i].items, batches[i].err = sources[i].search(ctx, client, query)
			}
		})
	}
	workers.Wait()

	var failures []error
	var count, mostResults int
	for i, result := range batches {
		if result.err != nil {
			failures = append(failures, fmt.Errorf("%s：%w", sources[i].Name, result.err))
			continue
		}
		count += len(result.items)
		mostResults = max(mostResults, len(result.items))
	}
	matches := make([]searchMatch, 0, count)
	// Show each source's top match before its lower-ranked matches.
	for rank := range mostResults {
		for i, result := range batches {
			if rank < len(result.items) {
				matches = append(matches, searchMatch{
					Item: result.items[rank], SourceID: sources[i].ID, SourceName: sources[i].Name,
				})
			}
		}
	}
	return matches, failures
}
