package main

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestZaimanhuaImportAndRead(t *testing.T) {
	client := &http.Client{Transport: cimocTestTransport(func(req *http.Request) (*http.Response, error) {
		var body string
		params := req.URL.Query()
		switch req.URL.Path {
		case "/":
			body = `<html><title>再漫画</title></html>`
		case "/sources.json":
			body = `{"version":1,"sources":[{"id":"cimoc-HAOMAN8","name":"再漫画","baseUrl":"https://manhua.zaimanhua.com"}]}`
		case "/app/v1/search/index":
			if params.Get("keyword") != "纸灯 & 星星" {
				t.Errorf("search keyword = %q", params.Get("keyword"))
			}
			body = `{"errno":0,"data":{"list":[{"title":"纸灯","comic_py":"zhideng","cover":"/images/cover.jpg"}]}}`
		case "/api/v1/comic2/comic/detail":
			if params.Get("comic_py") != "zhideng" {
				t.Errorf("comic slug = %q", params.Get("comic_py"))
			}
			body = `{"errno":0,"data":{"comicInfo":{"id":17,"title":"纸灯","description":"夜里的灯","cover":"/images/cover.jpg",
				"chapterList":[{"data":[{"chapter_id":23,"chapter_title":"第1话"}]}]}}}`
		case "/api/v1/comic2/chapter/detail":
			if params.Get("comic_id") != "17" || params.Get("chapter_id") != "23" {
				t.Errorf("chapter parameters = %v", params)
			}
			body = `{"errno":0,"data":{"chapterInfo":{"page_url":["https://images.zaimanhua.com/first.jpg?sign=one","https://images.zaimanhua.com/second.jpg?sign=two"]}}}`
		default:
			t.Fatalf("unexpected request: %s", req.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Request: req, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	cimoc, recognized := importCimoc([]byte(`{"HAOMAN8":{"baseUrl":"https://m.zaimanhua.com","parseImageUrl":"image-loading"}}`), false)
	if !recognized || len(cimoc.Sources) != 1 || len(cimoc.Skipped) != 0 {
		t.Fatalf("Cimoc import = %+v, recognized = %t", cimoc, recognized)
	}
	for _, address := range []string{zaimanhuaHome + "/", "https://subscription.example/sources.json"} {
		imported, err := importSources(t.Context(), client, address)
		if err != nil || len(imported.Sources) != 1 || imported.Sources[0].ID != cimoc.Sources[0].ID {
			t.Fatalf("import %s = %+v, error = %v", address, imported, err)
		}
	}
	src := cimoc.Sources[0]
	comics, err := src.search(t.Context(), client, "纸灯 & 星星")
	if err != nil || len(comics) != 1 || comics[0].Title != "纸灯" || comics[0].Cover != zaimanhuaHome+"/images/cover.jpg" {
		t.Fatalf("search = %+v, error = %v", comics, err)
	}
	detail, chapters, err := src.loadComic(t.Context(), client, comics[0])
	if err != nil || detail.Summary != "夜里的灯" || detail.Cover != comics[0].Cover ||
		len(chapters) != 1 || chapters[0].Title != "第1话" {
		t.Fatalf("detail = %+v, chapters = %+v, error = %v", detail, chapters, err)
	}
	pages, err := fetchPages(t.Context(), src, client, chapters[0])
	if err != nil || len(pages) != 2 || !strings.HasSuffix(pages[0].URL, "first.jpg?sign=one") || !strings.HasSuffix(pages[1].URL, "second.jpg?sign=two") {
		t.Fatalf("pages = %+v, error = %v", pages, err)
	}
}
