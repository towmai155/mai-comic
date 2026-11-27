package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestImportJMTTShareLink(t *testing.T) {
	const shareURL = "https://" + jmttShareHost + "/test-share"
	client := &http.Client{Transport: cimocTestTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() == shareURL {
			return &http.Response{
				StatusCode: http.StatusFound,
				Header:     http.Header{"Location": {"https://comic18j-susu.com/"}},
				Body:       http.NoBody,
				Request:    req,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("<html><title>漫画站</title></html>")),
			Request:    req,
		}, nil
	})}
	result, err := importSources(t.Context(), client, shareURL)
	if err != nil || len(result.Sources) != 1 {
		t.Fatalf("import result %+v, error = %v", result, err)
	}
	src := result.Sources[0]
	if src.ID != "cimoc-JMTT" || src.readsInBrowser() || src.chapterParser() == nil || src.BaseURL != "https://comic18j-susu.com" {
		t.Fatalf("unexpected source %+v", src)
	}
	unrelated, _ := url.Parse("https://example.com/")
	destination, _ := url.Parse(src.BaseURL)
	if _, ok := importJMTTWebsite(unrelated, destination); ok {
		t.Fatal("accepted an unrelated redirect as a JMTT subscription")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /search/photos", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("search_query") != "纸灯" {
			t.Error("search query missing")
		}
		io.WriteString(w, `<main id="wrapper"><div class="list-col"><div class="thumb-overlay">
			<a href="/album/1">封面</a></div><span class="video-title">纸灯</span></div></main>`)
	})
	mux.HandleFunc("GET /album/1", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `<h1 id="book-name">纸灯</h1>
			<a class="reading" href="/photo/1">开始阅读</a>
			<div id="episode-block"><a data-album="1" href="/photo/1">第一章</a></div>`)
	})
	mux.HandleFunc("GET /album/2", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `<h1 id="book-name">单章试读</h1><a class="reading" href="/photo/2">开始阅读</a>`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	src.BaseURL = server.URL
	hits, err := src.search(t.Context(), server.Client(), "纸灯")
	if err != nil || len(hits) != 1 || hits[0].Title != "纸灯" {
		t.Fatalf("search results %+v, error = %v", hits, err)
	}
	for _, comicURL := range []string{hits[0].URL, server.URL + "/album/2"} {
		chapters, err := src.Chapters.run(t.Context(), server.Client(), comicURL, "", false)
		if err != nil || len(chapters) != 1 || !strings.HasPrefix(chapters[0].URL, server.URL+"/photo/") {
			t.Fatalf("chapter results %+v, error = %v", chapters, err)
		}
	}
}
