package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestCimocAddressOnlyAdapter(t *testing.T) {
	result, recognized := importCimoc([]byte(`{
		"WEBTOONDONGMANMANHUA": "https://www.dongmanmanhua.cn",
		"MANHUAWU": {"baseUrl": "https://mhua5.com"},
		"UNKNOWN": "https://example.test"
	}`), false)
	if !recognized || len(result.Sources) != 2 || len(result.Skipped) != 1 {
		t.Fatalf("import result: %+v, recognized = %t", result, recognized)
	}
	if entry := result.Skipped[0]; entry.ID != "UNKNOWN" || entry.Reason == "" {
		t.Fatalf("missing diagnostic for address-only source: %+v", entry)
	}
	src := result.Sources[1]
	if src.ID != "cimoc-WEBTOONDONGMANMANHUA" || src.BaseURL != "https://www.dongmanmanhua.cn" ||
		result.Sources[0].BaseURL != "https://mhua5.com" {
		t.Fatalf("adapter did not keep the subscribed source IDs and addresses: %+v", result.Sources)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /search", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("keyword") != "纸灯" {
			t.Error("search did not send the keyword")
		}
		io.WriteString(w, `<ul class="card_lst"><li><a class="card_item" href="/episodeList?titleNo=1"><p class="subj">纸灯</p></a></li></ul>`)
	})
	mux.HandleFunc("GET /episodeList", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/comic/list?title_no=1", http.StatusFound)
	})
	mux.HandleFunc("GET /comic/list", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			io.WriteString(w, `<ul id="_listUl"><li><a href="/comic/viewer?episode_no=1"><span class="subj">第一话</span></a></li></ul>
				<div class="paginate"><a href="?title_no=1"><span>1</span></a><a href="#"><span class="on">2</span></a></div>`)
			return
		}
		io.WriteString(w, `<h1 class="subj">纸灯</h1><p class="summary">夜里的灯</p>
			<ul id="_listUl"><li><a href="/comic/viewer?episode_no=2"><span class="subj">第二话</span></a></li></ul>
			<div class="paginate"><a href="#"><span class="on">1</span></a><a href="?title_no=1&amp;page=2"><span>2</span></a></div>`)
	})
	mux.HandleFunc("GET /comic/viewer", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `<div id="_imageList"><img data-url="/page-one.jpg"></div><img data-url="/thumbnail.jpg">`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	src.BaseURL = server.URL
	client, ctx := server.Client(), t.Context()
	items, err := src.search(ctx, client, "纸灯")
	if err != nil || len(items) != 1 || items[0].Title != "纸灯" {
		t.Fatalf("search results: %+v, error = %v", items, err)
	}
	chapters, err := src.Chapters.run(ctx, client, items[0].URL, "", false)
	if err != nil || len(chapters) != 2 || chapters[1].Title != "第一话" {
		t.Fatalf("chapters: %+v, error = %v", chapters, err)
	}
	pages, err := fetchPages(ctx, src, client, chapters[1])
	if err != nil || len(pages) != 1 || pages[0].URL != server.URL+"/page-one.jpg" {
		t.Fatalf("pages: %+v, error = %v", pages, err)
	}
}

func TestCimocWebsiteFallback(t *testing.T) {
	result, recognized := importCimoc([]byte(`{
		"IKANMAN": "https://www.manhuagui.com",
		"IKANMANSERVER": "https://images.example.test",
		"WEBTOON": "https://example.test/config.json"
	}`), true)
	if !recognized || len(result.Sources) != 0 || len(result.Skipped) != 3 {
		t.Fatalf("unexpected import result: %+v", result)
	}
	for _, entry := range result.Skipped {
		want := ""
		if entry.ID == "IKANMAN" {
			want = "https://www.manhuagui.com"
		}
		if entry.Website != want {
			t.Errorf("website for %s = %q, want %q", entry.ID, entry.Website, want)
		}
	}
}

func TestManmanPublicChapterEntry(t *testing.T) {
	src, ok := adaptCimocSource("MANMANMANHUA", "https://www.manmanapp.com")
	if !ok {
		t.Fatal("Manman adapter missing")
	}
	current, _ := url.Parse(src.BaseURL + "/comic-24.html")
	chapters, _, err := src.Chapters.parsePage([]byte(`
		<div class="cartoon"><a class="start_read" href="/comic/detail-1.html">开始阅读</a></div>
		<div class="catalog"><ul class="comic_list"><li>
			<a href="/comic/detail-30.html"><h3>第三十话</h3><span>人气</span></a>
		</li></ul></div>
	`), current, false)
	if err != nil || len(chapters) != 2 || chapters[0].Title != "开始阅读" || chapters[1].Title != "第三十话" {
		t.Fatalf("chapters %+v, err = %v", chapters, err)
	}
}

func TestCimocKnownAliasIsOneSource(t *testing.T) {
	result, recognized := importCimoc([]byte(`{
		"DM5": "https://m.dm5.com",
		"DongManWu": {"baseUrl":"https://m.dm5.com"},
		"TENCENTM": "http://m.ac.qq.com"
	}`), true)
	if !recognized || len(result.Sources) != 2 || len(result.Skipped) != 0 {
		t.Fatalf("import result %+v", result)
	}
	if result.Sources[0].ID != "cimoc-DM5" || result.Sources[1].ID != "cimoc-TENCENT" ||
		result.Sources[1].BaseURL != "https://ac.qq.com" {
		t.Fatalf("aliases did not use their canonical sources: %+v", result.Sources)
	}
}

func TestImportCimocAndRead(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	mux.HandleFunc("GET /sources", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{
			"DEMO": {
				"name": "Cimoc 试读",
				"baseUrl": %q,
				"search": "/search/%%s/%%d",
				"searchInfoList": ".result",
				"searchInfoCid": "a:eq(0)",
				"searchInfoTitle": "a:eq(0)",
				"parseInfoTitle": "h1",
				"parseInfoIntro": ".summary",
				"parseChapterList1": ".chapter",
				"parseChapterList2": ".extra",
				"parseChapterPath": "a",
				"parseChapterTitle": "span",
				"parseImageList": "img.page",
				"parseImageUrl": "data-echo"
			},
			"ADDRESS_ONLY": %q,
			"REQUIRES_PARSER": {"baseUrl": %q},
			"revision": 42
		}`, server.URL, server.URL, server.URL)
	})
	mux.HandleFunc("GET /search/{query}/1", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("query") != "纸灯" {
			http.Error(w, "unexpected search query", http.StatusBadRequest)
			return
		}
		io.WriteString(w, `<div class="result"><a href="/comic">纸灯</a><a href="/other">其他</a></div>`)
	})
	mux.HandleFunc("GET /comic", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `<h1>纸灯</h1><p class="summary">夜里的灯</p>
			<div class="chapter"><a href="/chapter/1"><span>第一章</span></a></div>
			<div class="extra"><a href="/chapter/2"><span>番外</span></a></div>`)
	})
	mux.HandleFunc("GET /chapter/1", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `<img class="page" src="/placeholder" data-echo="../image.png">`)
	})

	ctx := t.Context()
	downloadURL, err := url.Parse(server.URL + "/sources")
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: cimocTestTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != cimocRawURL {
			return nil, fmt.Errorf("unexpected import URL %s", req.URL)
		}
		local := req.Clone(req.Context())
		local.URL = downloadURL
		return server.Client().Transport.RoundTrip(local)
	})}
	result, err := importSources(ctx, client, cimocLegacyURL)
	if err != nil || len(result.Sources) != 1 || len(result.Skipped) != 2 {
		t.Fatalf("imported %+v, error = %v", result, err)
	}
	src := result.Sources[0]
	if src.ID != "cimoc-DEMO" || src.Name != "Cimoc 试读" {
		t.Fatalf("source = %+v", src)
	}
	client = server.Client()
	items, err := src.search(ctx, client, "纸灯")
	if err != nil || len(items) != 1 || items[0].Title != "纸灯" {
		t.Fatalf("search = %+v, error = %v", items, err)
	}
	details, err := src.Details.run(ctx, client, items[0].URL, "", true)
	if err != nil || len(details) != 1 || details[0].Summary != "夜里的灯" {
		t.Fatalf("details = %+v, error = %v", details, err)
	}
	chapters, err := src.Chapters.run(ctx, client, items[0].URL, "", false)
	if err != nil || len(chapters) != 2 || chapters[0].Title != "第一章" || chapters[1].Title != "番外" {
		t.Fatalf("chapters = %+v, error = %v", chapters, err)
	}
	pages, err := fetchPages(ctx, src, client, chapters[0])
	if err != nil || len(pages) != 1 || pages[0].URL != server.URL+"/image.png" {
		t.Fatalf("pages = %+v, error = %v", pages, err)
	}
}

type cimocTestTransport func(*http.Request) (*http.Response, error)

func (f cimocTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
