package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestJMTTChapterUsesPublishedImages(t *testing.T) {
	src := source{ID: "cimoc-JMTT", BaseURL: "https://" + jmttHost, BrowserReader: true}
	if src.readsInBrowser() {
		t.Fatal("previously imported source should use the built-in reader")
	}
	const publishedHost = "https://cdn-msp3." + jmttHost
	const chapter = "https://" + jmttHost + "/photo/240000/"
	client := &http.Client{Transport: cimocTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != chapter {
			return nil, fmt.Errorf("unexpected URL %s", r.URL)
		}
		body := `<script>var scramble_id = 220980;</script>
			<img data-original="` + publishedHost + `/cover.jpg">
			<div class="scramble-page"><img data-original="` + publishedHost + `/media/photos/240000/00001.webp?v=1"></div>
			<div class="scramble-page"><img data-original="` + publishedHost + `/media/photos/240000/00002.gif?v=1"></div>
			<div class="scramble-page"><img data-original="` + publishedHost + `/media/photos/1/wrong-photo.jpg"></div>`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	pages, err := fetchPages(t.Context(), src, client, item{URL: chapter})
	want := []item{
		{URL: publishedHost + "/media/photos/240000/00001.webp?v=1#mai-jmtt-strips=10"},
		{URL: publishedHost + "/media/photos/240000/00002.gif?v=1"},
	}
	if err != nil || !reflect.DeepEqual(pages, want) {
		t.Fatalf("pages = %+v, error = %v", pages, err)
	}
}

func TestJMTTOldDirectoryUsesNewChapterHost(t *testing.T) {
	const old = "https://" + jmttHost
	const current = "https://" + jmttFufiHost
	const image = "https://cdn-msp3." + jmttFufiHost + "/media/photos/240000/00001.webp"
	src, ok := adaptCimocSource("JMTT", old)
	if !ok {
		t.Fatal("old source is no longer recognized")
	}
	client := &http.Client{Transport: cimocTestTransport(func(r *http.Request) (*http.Response, error) {
		body := ""
		switch r.URL.String() {
		case old + "/album/240000":
			return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {current + "/album/240000"}}, Body: http.NoBody, Request: r}, nil
		case current + "/album/240000":
			body = `<h1 id="book-name">试读</h1><a class="reading" href="/photo/240000">第一章</a>`
		case current + "/photo/240000":
			return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {"https://" + jmttNewHost + "/photo/240000"}}, Body: http.NoBody, Request: r}, nil
		case "https://" + jmttNewHost + "/photo/240000":
			body = `<script>var scramble_id = 220980;</script><div class="scramble-page"><img data-original="` + image + `"></div>`
		default:
			return nil, fmt.Errorf("unexpected URL %s", r.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	_, chapters, err := src.loadComic(t.Context(), client, item{URL: old + "/album/240000"})
	if err != nil || len(chapters) != 1 || chapters[0].URL != current+"/photo/240000" {
		t.Fatalf("chapters = %+v, error = %v", chapters, err)
	}
	pages, err := fetchPages(t.Context(), src, client, chapters[0])
	if err != nil || len(pages) != 1 || pages[0].URL != image+"#mai-jmtt-strips=10" {
		t.Fatalf("pages = %+v, error = %v", pages, err)
	}
}

func TestJMTTImageRestoresStripOrder(t *testing.T) {
	scrambled := image.NewNRGBA(image.Rect(0, 0, 2, 7))
	for y := range 7 {
		for x := range 2 {
			scrambled.SetNRGBA(x, y, color.NRGBA{R: uint8(y * 10), A: 255})
		}
	}
	var source bytes.Buffer
	if err := png.Encode(&source, scrambled); err != nil {
		t.Fatal(err)
	}
	response := &http.Response{Body: io.NopCloser(&source), Header: http.Header{}}
	response, err := restoreJMTTResponse(response, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	restored, err := png.Decode(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	wantRows := []int{3, 4, 5, 6, 2, 1, 0}
	for y, sourceRow := range wantRows {
		want := scrambled.At(0, sourceRow)
		if got := restored.At(0, y); color.NRGBAModel.Convert(got) != color.NRGBAModel.Convert(want) {
			t.Errorf("row %d = %v, want %v", y, got, want)
		}
	}
}

func TestManga1234ParsesPublishedReaderPage(t *testing.T) {
	src := source{ID: "cimoc-DMZJFIX", BaseURL: "https://www.wmh1234.com", BrowserReader: true}
	if src.readsInBrowser() {
		t.Fatal("previously imported source should use the built-in reader")
	}
	client := &http.Client{Transport: cimocTestTransport(func(r *http.Request) (*http.Response, error) {
		var body string
		switch r.URL.String() {
		case "https://www.wmh1234.com/comic/1/2.html":
			body = `<script>location.replace("https://reader.hqread.cc/r/public-reader");</script>`
		case "https://reader.hqread.cc/r/public-reader":
			body = `<img data-src="/ui/placeholder.png">
				<img class="reader-image" data-pic-id="1" data-src="/page-one.webp">
				<img class="reader-image" data-pic-id="2" data-src="/page-two.webp">`
		default:
			return nil, fmt.Errorf("unexpected request: %s", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	pages, err := fetchPages(t.Context(), src, client, item{URL: "https://www.wmh1234.com/comic/1/2.html"})
	want := []item{{URL: "https://reader.hqread.cc/page-one.webp"}, {URL: "https://reader.hqread.cc/page-two.webp"}}
	if err != nil || !reflect.DeepEqual(pages, want) {
		t.Fatalf("pages = %+v, error = %v", pages, err)
	}
}

func TestDM5ReadsPublicChapterBatches(t *testing.T) {
	src := source{ID: "cimoc-DM5", BaseURL: "https://m.dm5.com"}
	const desktop = "https://www.dm5.com/m31/"
	public := true
	var requestedPages []string
	client := &http.Client{Transport: cimocTestTransport(func(r *http.Request) (*http.Response, error) {
		var body string
		switch r.URL.Path {
		case "/m31/":
			body = fmt.Sprintf(`<script>var DM5_CID=31;var DM5_MID=10;var DM5_IMAGE_COUNT=3;var DM5_ISNEED="%t";var DM5_VIEWSIGN_DT="today";var DM5_VIEWSIGN="issued";</script>`, !public)
		case "/m31/chapterfun.ashx":
			query := r.URL.Query()
			if r.Referer() != desktop || query.Get("_sign") != "issued" {
				return nil, fmt.Errorf("missing public viewer parameters")
			}
			page := query.Get("page")
			requestedPages = append(requestedPages, page)
			switch page {
			case "1":
				body = `eval(function(){ }('0 1="https://img.cdndm5.com/";0 2=["first.jpg","second.jpg"];2[i]=1+2[i]+\'?cid=31&key=first-batch\';',36,3,'var|pix|pvalue'.split('|'),0,{}))`
			case "3":
				if query.Get("key") != "first-batch" {
					return nil, fmt.Errorf("missing returned batch key")
				}
				body = `var pix="https://img.cdndm5.com/"; var pvalue=["third.jpg"]; pvalue[i]=pix+pvalue[i]+'?cid=31&key=last-batch';`
			default:
				return nil, fmt.Errorf("unexpected page request %s", page)
			}
		default:
			return nil, fmt.Errorf("unexpected request %s", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	chapter := item{URL: "https://m.dm5.com/m31/"}
	pages, err := fetchPages(t.Context(), src, client, chapter)
	want := []item{
		{URL: "https://img.cdndm5.com/first.jpg?cid=31&key=first-batch"},
		{URL: "https://img.cdndm5.com/second.jpg?cid=31&key=first-batch"},
		{URL: "https://img.cdndm5.com/third.jpg?cid=31&key=last-batch"},
	}
	if err != nil || !reflect.DeepEqual(pages, want) || !reflect.DeepEqual(requestedPages, []string{"1", "3"}) {
		t.Fatalf("pages = %+v, requested pages = %v, error = %v", pages, requestedPages, err)
	}
	public = false
	requestedPages = nil
	if _, err := fetchPages(t.Context(), src, client, chapter); err == nil || len(requestedPages) != 0 {
		t.Fatalf("unavailable chapter requested image batches: %v, error = %v", requestedPages, err)
	}
}
