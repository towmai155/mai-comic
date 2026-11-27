package main

import (
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/netimage"
	"mygo-manga/internal/demosource"
)

func TestSearchShowsMatchesFromMultipleSources(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /one", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `[{"title":"同名漫画","url":"/one/comic"},{"title":"同名漫画外传","url":"/one/other"}]`)
	})
	mux.HandleFunc("GET /two", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `[{"title":"同名漫画","url":"/two/comic"}]`)
	})
	mux.HandleFunc("GET /unavailable", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	var sources []source
	for i, id := range []string{"one", "two", "unavailable"} {
		sources = append(sources, source{
			ID: id, Name: []string{"甲图源", "乙图源", "丙图源"}[i], BaseURL: server.URL,
			Search: rule{Format: "json", URL: "/" + id + "?q={query}",
				Fields: map[string]string{"title": "title", "url": "url"}},
		})
	}
	matches, failures := searchSources(t.Context(), server.Client(), sources, "同名漫画")
	if len(failures) != 1 || len(matches) != 3 {
		t.Fatalf("matches = %+v, failures = %v", matches, failures)
	}
	if matches[0].SourceID != "one" || matches[1].SourceID != "two" ||
		matches[1].Item.URL != server.URL+"/two/comic" || matches[2].SourceID != "one" {
		t.Fatalf("matches are missing their source or are out of order: %+v", matches)
	}
	a := app{
		library: library{Sources: sources}, results: matches, searched: true, searchedQuery: "同名漫画", chapter: -1,
		statePath: filepath.Join(t.TempDir(), "library.json"),
	}
	tt := ui.NewTester(a.view, 1040, 760)
	if !tt.HasText("全部图源") || !tt.HasText("同名漫画 · 甲图源") || !tt.HasText("同名漫画 · 乙图源") {
		t.Fatalf("aggregate results are missing source labels: %v", tt.Texts())
	}
	if err := tt.Click("严格搜索"); err != nil {
		t.Fatal(err)
	}
	if tt.HasText("同名漫画外传") || !tt.HasText("同名漫画 · 甲图源") || !tt.HasText("同名漫画 · 乙图源") {
		t.Fatalf("strict search did not keep only exact titles from both sources: %v", tt.Texts())
	}
	saved, err := readLibrary(a.statePath)
	if err != nil || !saved.StrictSearch {
		t.Fatalf("strict search setting was not saved: %v", err)
	}
	if err := tt.Click("严格搜索"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("同名漫画外传") {
		t.Fatalf("regular search results were not restored: %v", tt.Texts())
	}
}

func TestSourceImportAndRead(t *testing.T) {
	server := httptest.NewServer(demosource.Handler())
	defer server.Close()
	client := server.Client()
	ctx := t.Context()
	result, err := importSources(ctx, client, server.URL+"/sources.json")
	if err != nil || len(result.Sources) != 2 || len(result.Skipped) != 0 {
		t.Fatalf("imported %d sources, skipped %d, err = %v", len(result.Sources), len(result.Skipped), err)
	}

	for i, example := range []struct {
		query, title, cover string
		chapters            int
		pages               int
	}{{"远足", "纸上的远足", "/page/1.png", 2, 4}, {"纸灯", "纸灯", "/page/8.png", 1, 3}} {
		t.Run(result.Sources[i].ID, func(t *testing.T) {
			src := result.Sources[i]
			results, err := src.search(ctx, client, example.query)
			if err != nil || len(results) != 1 || results[0].Title != example.title {
				t.Fatalf("search results %v, err = %v", results, err)
			}
			comic := results[0]
			if comic.Cover != server.URL+example.cover {
				t.Fatalf("search cover = %q", comic.Cover)
			}
			details, err := src.Details.run(ctx, client, comic.URL, "", true)
			if err != nil || len(details) != 1 || details[0].Title != example.title ||
				details[0].Summary == "" || details[0].Cover != server.URL+example.cover {
				t.Fatalf("details %v, err = %v", details, err)
			}
			chapters, err := src.Chapters.run(ctx, client, comic.URL, "", false)
			if err != nil || len(chapters) != example.chapters {
				t.Fatalf("chapters %v, err = %v", chapters, err)
			}
			pages, err := fetchPages(ctx, src, client, chapters[0])
			if err != nil || len(pages) != example.pages {
				t.Fatalf("pages %v, err = %v", pages, err)
			}
			target, err := resolveURL(nil, pages[0].URL)
			if err != nil || target.Host != string(server.Listener.Addr().String()) {
				t.Fatalf("resolved first image %v, err = %v", target, err)
			}
			body, _, err := fetch(ctx, client, target, 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ui.DecodeImage(body); err != nil {
				t.Fatal(err)
			}
		})
	}

	if _, err := importSources(ctx, client, server.URL+"/html/comic"); err == nil {
		t.Fatal("import accepted an HTML page as a source configuration")
	}
}

func TestSearchResultsRenderDownloadedCover(t *testing.T) {
	cover := image.NewPaletted(image.Rect(0, 0, 54, 76), color.Palette{
		color.RGBA{R: 230, G: 43, B: 21, A: 255},
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if err := png.Encode(w, cover); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	defer netimage.Forget(server.URL)

	a := app{chapter: -1, phone: true, searched: true, library: library{
		Sources: []source{{ID: "demo", Name: "试读"}},
	}}
	a.results = []searchMatch{
		{SourceID: "demo", SourceName: "试读", Item: item{Title: "纸灯", URL: server.URL + "/comic", Cover: server.URL}},
		{SourceID: "demo", SourceName: "试读", Item: item{Title: "无封面漫画", URL: server.URL + "/other"}},
	}
	tt := ui.NewTester(a.view, 360, 780)
	bounds, ok := tt.Find("纸灯 · 试读")
	if !ok || !tt.HasText("无封面漫画") || !tt.HasText("暂无封面") {
		t.Fatalf("search cards are missing their labels or placeholder: %v", tt.Texts())
	}
	x, y := int(bounds.X+37), int(bounds.Y+45)
	deadline := time.NewTimer(time.Minute)
	defer deadline.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		if pixel := tt.Image().RGBAAt(x, y); pixel.R > 180 && pixel.G < 100 {
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("the search result never displayed its downloaded cover")
		case <-tick.C:
			tt.Frame()
		}
	}
}

func TestBrowserSourceOpensChapter(t *testing.T) {
	src := cimocAdapters["WEBTOON"].rules
	src.ID = "cimoc-WEBTOON"
	const chapterURL = "https://www.webtoons.com/en/test/viewer?episode_no=1"
	a := app{
		statePath: filepath.Join(t.TempDir(), "library.json"),
		library:   library{Sources: []source{src}}, comicSource: src.ID, chapter: -1,
		comic:  item{Title: "试读", URL: "https://www.webtoons.com/en/test/list"},
		detail: item{Title: "试读"}, chapters: []item{{Title: "第一章", URL: chapterURL}},
	}
	tt := ui.NewTester(a.view, 1040, 760)
	if !tt.HasText("章节 · 在浏览器打开") {
		t.Fatalf("missing browser reading label: %v", tt.Texts())
	}
	if err := tt.Click("第一章"); err != nil {
		t.Fatal(err)
	}
	if opened := tt.OpenedURLs(); len(opened) != 1 || opened[0] != chapterURL || a.chapter != -1 {
		t.Fatalf("opened: %v, native chapter: %d", opened, a.chapter)
	}
	if len(a.library.History) != 1 || a.library.History[0].Page != -1 {
		t.Fatalf("browser reading was not recorded: %+v", a.library.History)
	}
}

func TestLongComicSummaryStaysAboveChapters(t *testing.T) {
	summary := strings.Repeat("这是一段需要在手机屏幕上换行的作品简介。", 22)
	a := app{
		phone: true, comic: item{URL: "https://example.test/comic"}, chapter: -1,
		detail:   item{Title: "试读", Summary: summary},
		chapters: []item{{Title: "第一章"}, {Title: "第二章"}},
	}
	tt := ui.NewTester(a.view, 320, 560)
	preview, hasPreview := tt.Find(summary)
	more, hasMore := tt.Find("查看完整简介")
	chapters, hasChapters := tt.Find("章节")
	first, hasFirst := tt.Find("第一章")
	if !hasPreview || !hasMore || !hasChapters || !hasFirst ||
		preview.Y+preview.H > more.Y || more.Y+more.H > chapters.Y || chapters.Y+chapters.H > first.Y {
		t.Fatalf("summary and chapters overlap or are missing: preview=%v more=%v chapters=%v first=%v, labels=%v", preview, more, chapters, first, tt.Texts())
	}
	if err := tt.Click("查看完整简介"); err != nil {
		t.Fatal(err)
	}
	if !a.summaryOpen || !tt.HasText("作品简介") {
		t.Fatal("full summary did not open")
	}
	tt.Key(0, ui.KeyBack)
	if a.summaryOpen || a.comic.URL == "" || !tt.HasText("第一章") {
		t.Fatal("Back did not return from the full summary to the chapter list")
	}
}

func TestChapterCatalogOrder(t *testing.T) {
	chapters := []item{
		{Title: "第十二话", URL: "https://example.test/12"},
		{Title: "番外", URL: "https://example.test/extra"},
		{Title: "Chapter 2.5", URL: "https://example.test/2.5"},
		{Title: "第一话", URL: "https://example.test/1"},
	}
	src := source{ID: "demo", Name: "试读图源", BrowserReader: true}
	a := app{
		phone: true, statePath: filepath.Join(t.TempDir(), "library.json"),
		library: library{Sources: []source{src}}, comicSource: src.ID, chapter: -1,
		comic: item{Title: "试读", URL: "https://example.test/comic"}, detail: item{Title: "试读"},
		chapters: slices.Clone(chapters), sourceNewestFirst: newestChapterFirst(chapters),
	}
	tt := ui.NewTester(a.view, 360, 780)
	assertOrder := func(want []string) {
		t.Helper()
		var first, previous ui.Rect
		for i, title := range want {
			cell, found := tt.Find(title)
			if !found || (i > 0 && i < 3 && (cell.Y != first.Y || cell.X < previous.X+previous.W)) ||
				(i == 3 && (cell.Y <= first.Y || cell.X != first.X || cell.W != first.W)) {
				t.Fatalf("expected %v in screen order, got %v", want, tt.Texts())
			}
			if i == 0 {
				first = cell
			}
			previous = cell
		}
	}
	assertOrder([]string{"第一话", "Chapter 2.5", "番外", "第十二话"})
	if err := tt.Click("倒序"); err != nil {
		t.Fatal(err)
	}
	assertOrder([]string{"第十二话", "番外", "Chapter 2.5", "第一话"})
	saved, err := readLibrary(a.statePath)
	if err != nil || !saved.ChaptersDescending {
		t.Fatalf("chapter order was not saved: %v", err)
	}
	if err := tt.Click("正序"); err != nil {
		t.Fatal(err)
	}
	assertOrder([]string{"第一话", "Chapter 2.5", "番外", "第十二话"})
	if err := tt.Click("第一话"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(tt.OpenedURLs(), []string{chapters[3].URL}) || !slices.Equal(a.chapters, chapters) {
		t.Fatalf("display order affected chapter links or reader order: %v, %v", tt.OpenedURLs(), a.chapters)
	}
}

func TestChapterGridLongPressOpensOriginalSite(t *testing.T) {
	src, ok := adaptCimocSource("MKZHAN", "https://www.mkzhan.com")
	if !ok {
		t.Fatal("missing test source adapter")
	}
	const chapterURL = "https://www.mkzhan.com/123/456.html"
	const chapterTitle = "第一话 这是在手机屏幕上需要省略的很长章节标题"
	a := app{
		phone: true, statePath: filepath.Join(t.TempDir(), "library.json"), chapter: -1,
		library: library{Sources: []source{src}}, comicSource: src.ID,
		comic: item{Title: "试读", URL: "https://www.mkzhan.com/123/"}, detail: item{Title: "试读"},
		chapters: []item{{Title: chapterTitle, URL: chapterURL}},
	}
	tt := ui.NewTester(a.view, 320, 560)
	cell, found := tt.Find(chapterTitle)
	if !found || cell.W > 100 || tt.HasText("在原站打开") {
		t.Fatalf("expected one compact chapter button: cell=%v, labels=%v", cell, tt.Texts())
	}
	x, y := cell.X+cell.W/2, cell.Y+cell.H/2
	tt.FingerDown(x, y)
	tt.Advance(600 * time.Millisecond)
	tt.FingerUp(x, y)
	if err := tt.Click("在原站打开"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(tt.OpenedURLs(), []string{chapterURL}) {
		t.Fatalf("opened URLs = %v", tt.OpenedURLs())
	}
}

func TestReaderFollowsChapterOrderFromNewestFirstSource(t *testing.T) {
	a := app{
		phone: true, chapter: 1, sourceNewestFirst: true, lastPageReady: true,
		chapters: []item{{Title: "第三章"}, {Title: "第二章"}, {Title: "第一章"}},
		pages:    []item{{}},
	}
	var opened []int
	tt := ui.NewTester(func(c *ui.Context) {
		a.readToolbar(c, true, func(index int) { opened = append(opened, index) })
	}, 360, 780)
	for _, title := range []string{"上一章", "下一章"} {
		if err := tt.Click(title); err != nil {
			t.Fatal(err)
		}
	}
	if !slices.Equal(opened, []int{2, 0}) || !a.markChapterEnd(1) {
		t.Fatalf("previous/next chapter index is wrong: %v", opened)
	}
	a.chapter, a.autoNextTried = 0, false
	if a.markChapterEnd(0) {
		t.Fatal("latest chapter should not advance")
	}
}

func TestReadingPositionSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.json")
	a := app{
		statePath:   path,
		comicSource: "demo-json",
		library:     library{Sources: []source{{ID: "demo-json"}}},
		comic:       item{Title: "纸上的远足", URL: "https://example.test/comic"},
		chapters:    []item{{Title: "第一章", URL: "https://example.test/chapter"}},
	}
	pages := []item{{}, {}, {}, {}}
	a.showChapter(0, pages, 0)
	a.firstPage = 2
	a.remember()
	saved, err := readLibrary(path)
	if err != nil || saved.Last == nil || saved.Last.Page != 2 || saved.Last.Chapter != a.chapters[0] {
		t.Fatalf("saved %#v, err = %v", saved.Last, err)
	}
	if len(saved.Sources) != 1 || saved.Sources[0].ID != a.comicSource || saved.Last.SourceID != a.comicSource {
		t.Fatalf("lost sources: %v", saved.Sources)
	}

	restarted := app{statePath: path, comicSource: saved.Last.SourceID, library: saved, comic: saved.Last.Comic,
		detail: saved.Last.Comic, chapters: []item{saved.Last.Chapter}}
	restarted.showChapter(0, pages, saved.Last.Page)
	tt := ui.NewTester(restarted.view, 1040, 760)
	first, _ := restarted.reader.Visible()
	if first != 2 || tt.HasText("章节列表") || tt.HasText("图源") {
		t.Fatalf("reader did not restore page 3 in immersive mode: %v", tt.Texts())
	}
	tt.ClickAt(520, 350)
	if !tt.HasText("纸上的远足") || !tt.HasText("3 / 4 页") {
		t.Fatalf("reader did not restore page 3: %v", tt.Texts())
	}
	if err := tt.Click("章节列表"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("第一章") || !tt.HasText("返回搜索") {
		t.Fatalf("back to chapters displayed %v", tt.Texts())
	}
}

func TestPhoneSourcePicker(t *testing.T) {
	icon, err := loadAppIcon()
	if err != nil {
		t.Fatal(err)
	}
	a := app{
		appIcon: icon,
		chapter: -1,
		library: library{Sources: []source{{ID: "demo-json", Name: "试读 · JSON 图源"}}},
	}
	tt := ui.NewTester(a.view, 360, 780)
	if !tt.HasText("搜索漫画") || tt.HasText("图源配置 URL") {
		t.Fatalf("unexpected phone home: %v", tt.Texts())
	}
	if err := tt.Click("菜单"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("导入图源") || !tt.HasText("图源管理") || !tt.HasText("搜索漫画") {
		t.Fatalf("unexpected phone source picker: %v", tt.Texts())
	}
	if bounds, ok := tt.Find("麦依应用图标"); !ok || bounds.W != 44 || bounds.H != 44 {
		t.Fatalf("the drawer icon is missing or incorrectly sized: %+v", bounds)
	}
	if err := tt.Click("刷新图源"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("请粘贴原图源配置 URL，再点「刷新图源」。刷新成功后会记住地址。") {
		t.Fatalf("legacy library did not show how to refresh: %v", tt.Texts())
	}
	tt.Key(0, ui.KeyBack)
	if !tt.HasText("搜索漫画") || tt.HasText("图源配置 URL") {
		t.Fatalf("system back did not close the source picker: %v", tt.Texts())
	}
	if err := tt.Click("菜单"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("试读 · JSON 图源"); err != nil {
		t.Fatal(err)
	}
	if a.sourceID != "demo-json" || !tt.HasText("搜索漫画") || tt.HasText("图源配置 URL") {
		t.Fatalf("source selection did not return to search: %v", tt.Texts())
	}
	if err := tt.Click("菜单"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("全部图源"); err != nil {
		t.Fatal(err)
	}
	if a.sourceID != "" || !tt.HasText("搜索漫画") || !tt.HasText("全部图源") {
		t.Fatalf("all sources selection did not return to search: %v", tt.Texts())
	}
}

func TestPhoneAppearanceSwitchPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.json")
	a := app{phone: true, chapter: -1, section: historySection, statePath: path}
	tt := ui.NewTester(a.view, 360, 780)
	tt.SetDark(false)
	choose := func(label string) {
		t.Helper()
		for _, button := range []string{"菜单", label} {
			if err := tt.Click(button); err != nil {
				t.Fatal(err)
			}
		}
		tt.Key(0, ui.KeyBack)
	}
	checkBackground := func(tester *ui.Tester, want ui.Color) {
		t.Helper()
		got := tester.Image().RGBAAt(350, 740)
		if got.R != want.R || got.G != want.G || got.B != want.B {
			t.Fatalf("background = %v, want %v", got, want)
		}
	}

	choose("夜晚")
	checkBackground(tt, ui.DarkTheme().Background)
	saved, err := readLibrary(path)
	if err != nil || saved.Appearance != mygo.ThemeDark {
		t.Fatalf("saved appearance = %q, err = %v", saved.Appearance, err)
	}
	restarted := app{phone: true, chapter: -1, section: historySection, library: saved}
	reopened := ui.NewTester(restarted.view, 360, 780)
	reopened.SetDark(false)
	checkBackground(reopened, ui.DarkTheme().Background)

	tt.SetDark(true)
	choose("白天")
	checkBackground(tt, ui.LightTheme().Background)
	choose("跟随系统")
	checkBackground(tt, ui.DarkTheme().Background)
}

func TestRefreshSourceConfigsPersistsUpdatesAndKeepsFailedSources(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("/sources.json", demosource.Handler())
	mux.HandleFunc("/offline.json", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	successURL, failedURL := server.URL+"/sources.json", server.URL+"/offline.json"
	a := app{
		statePath:    filepath.Join(t.TempDir(), "library.json"),
		sourceID:     "demo-json",
		sourcePicker: true,
		results:      []searchMatch{{SourceID: "demo-json"}},
		searched:     true,
		library: library{
			SourceURLs: []string{failedURL},
			Sources:    []source{{ID: "demo-json", Name: "旧名称"}, {ID: "offline", Name: "暂不可用"}},
		},
	}
	updates := fetchSourceUpdates(t.Context(), server.Client(), []string{failedURL, successURL})
	a.applySourceRefresh(updates)
	data, err := readLibrary(a.statePath)
	if err != nil || !slices.Equal(data.SourceURLs, []string{failedURL, successURL}) {
		t.Fatalf("saved URLs = %v, err = %v", data.SourceURLs, err)
	}
	if len(data.Sources) != 3 || data.Sources[0].Name == "旧名称" ||
		data.Sources[1].ID != "offline" || data.Sources[2].ID != "demo-html" {
		t.Fatalf("unexpected refreshed sources: %+v", data.Sources)
	}
	if !a.sourcePicker || a.sourceID != "demo-json" || len(a.results) != 0 ||
		a.status != "已刷新 1 份配置，更新 2 个图源。另有 1 份配置刷新失败。" {
		t.Fatalf("unexpected state after refreshing: %s", a.status)
	}
	a.applySourceRefresh(updates)
	if !slices.Equal(a.library.SourceURLs, data.SourceURLs) || len(a.library.Sources) != 3 {
		t.Fatalf("second refresh duplicated sources or URLs: %+v", a.library)
	}
}

func TestLibraryMigratesAndKeepsOneEntryPerComicAndSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.json")
	comic := item{Title: "试读", URL: "https://example.test/comic"}
	first := progress{SourceID: "one", Comic: comic, Chapter: item{Title: "第一话", URL: "https://example.test/1"}, Page: 4}
	if err := writeLibrary(path, library{Last: &first}); err != nil {
		t.Fatal(err)
	}
	data, err := readLibrary(path)
	if err != nil || !slices.Equal(data.History, []progress{first}) {
		t.Fatalf("legacy history = %v, err = %v", data.History, err)
	}
	a := app{statePath: path, library: data}
	other := first
	other.SourceID = "two"
	a.recordProgress(other)
	first.Page++
	a.recordProgress(first)
	data, err = readLibrary(path)
	if err != nil || !slices.Equal(data.History, []progress{first, other}) || data.Last == nil || *data.Last != first {
		t.Fatalf("reopened history = %v, last = %v, err = %v", data.History, data.Last, err)
	}
	a.removeHistory(first)
	if a.library.Last == nil || *a.library.Last != other {
		t.Fatalf("removing the latest entry did not promote the other one: %v", a.library.Last)
	}
}

func TestPhoneFavoritesAndHistoryNavigation(t *testing.T) {
	covers := httptest.NewServer(demosource.Handler())
	defer covers.Close()
	cover := covers.URL + "/page/1.png"
	defer netimage.Forget(cover)
	comic := item{Title: "纸上的远足", URL: "https://example.test/comic"}
	chapter := item{Title: "第一章", URL: "https://example.test/1"}
	a := app{
		chapter: -1, comic: comic, detail: comic, comicSource: "demo",
		statePath: filepath.Join(t.TempDir(), "library.json"),
		library:   library{Sources: []source{{ID: "demo", Name: "试读图源"}}},
	}
	a.recordProgress(progress{SourceID: "demo", Comic: comic, Chapter: chapter, Page: -1})
	tt := ui.NewTester(a.view, 360, 780)
	click := func(label string) {
		t.Helper()
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	click("收藏漫画")
	data, err := readLibrary(a.statePath)
	if err != nil || len(data.Favorites) != 1 || data.Favorites[0].Comic.URL != comic.URL {
		t.Fatalf("saved favorites = %v, err = %v", data.Favorites, err)
	}
	click("返回搜索")
	click("收藏")
	if !tt.HasText(comic.Title) || !tt.HasText("试读图源") {
		t.Fatalf("favorite is missing source or title: %v", tt.Texts())
	}
	a.storeComicCover(coverKey{"demo", comic.URL}, cover)
	saved, err := readLibrary(a.statePath)
	if err != nil || saved.Favorites[0].Comic.Cover == "" ||
		saved.History[0].Comic.Cover != saved.Favorites[0].Comic.Cover {
		t.Fatalf("cover was not backfilled for both entries: %+v, err = %v", saved, err)
	}
	if tt.HasText("⋯") {
		t.Fatalf("the cover still has a visible options button: %v", tt.Texts())
	}
	card, ok := tt.Find(comic.Title)
	if !ok {
		t.Fatal("favorite card not found")
	}
	x, y := card.X+card.W/2, card.Y+card.H/2
	tt.FingerDown(x, y)
	tt.Advance(600 * time.Millisecond)
	tt.FingerUp(x, y)
	if !tt.HasText("取消收藏") || len(a.library.Favorites) != 1 || a.comic.URL != "" {
		t.Fatalf("a long press did not just open the favorite menu: %v", tt.Texts())
	}
	click("取消收藏")
	tt.Key(0, ui.KeyBack)
	click("历史")
	if !tt.HasText("试读图源") || !tt.HasText("第一章 · 原站阅读") {
		t.Fatalf("browser history is missing its chapter: %v", tt.Texts())
	}
	click(comic.Title)
	if !slices.Equal(tt.OpenedURLs(), []string{chapter.URL}) {
		t.Fatalf("history did not reopen the original chapter: %v", tt.OpenedURLs())
	}
	card, ok = tt.Find(comic.Title)
	if !ok {
		t.Fatal("history card not found")
	}
	x, y = card.X+card.W/2, card.Y+card.H/2
	tt.FingerDown(x, y)
	tt.Advance(600 * time.Millisecond)
	tt.FingerUp(x, y)
	if !tt.HasText("在应用内阅读") || !tt.HasText("在原站打开") || len(tt.OpenedURLs()) != 1 {
		t.Fatalf("history long press should open both reading options without a browser launch: %v", tt.Texts())
	}
	click("在原站打开")
	if !slices.Equal(tt.OpenedURLs(), []string{chapter.URL, chapter.URL}) {
		t.Fatalf("history menu did not open the original chapter: %v", tt.OpenedURLs())
	}
	a.recordProgress(progress{SourceID: "demo", Comic: comic, Chapter: chapter, Page: 4})
	if err := tt.RightClick(comic.Title); err != nil {
		t.Fatal(err)
	}
	click("在原站打开")
	if a.library.History[0].Page != 4 {
		t.Fatal("opening the original site discarded the saved page")
	}
	if err := tt.RightClick(comic.Title); err != nil {
		t.Fatal(err)
	}
	click("删除记录")
	data, err = readLibrary(a.statePath)
	if err != nil || len(data.Favorites) != 0 || len(data.History) != 0 || data.Last != nil {
		t.Fatalf("removal was not saved: %+v, err = %v", data, err)
	}
}

func TestPhoneLibraryUsesThreeCoverColumns(t *testing.T) {
	coverImage := image.NewPaletted(image.Rect(0, 0, 80, 120), color.Palette{
		color.RGBA{R: 225, G: 48, B: 40, A: 255},
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if err := png.Encode(w, coverImage); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	defer netimage.Forget(server.URL)

	a := app{chapter: -1, phone: true, section: favoritesSection, library: library{
		Sources: []source{{ID: "demo", Name: "试读图源"}},
	}}
	titles := []string{"纸灯", "放学之后", "出发"}
	for _, title := range titles {
		a.library.Favorites = append(a.library.Favorites, favorite{SourceID: "demo", Comic: item{
			Title: title, URL: server.URL + "/" + title, Cover: server.URL,
		}})
	}
	tt := ui.NewTester(a.view, 360, 780)
	var first ui.Rect
	var lastX float32
	for i, title := range titles {
		bounds, ok := tt.Find(title)
		if !ok || bounds.W == 0 || (i > 0 && (bounds.Y != first.Y || bounds.X <= lastX)) {
			t.Fatalf("card %q is out of the first row: %+v", title, bounds)
		}
		if i == 0 {
			first = bounds
		}
		lastX = bounds.X
	}
	x, y := int(first.X+first.W/2), int(first.Y+26)
	deadline := time.NewTimer(time.Minute)
	defer deadline.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		if pixel := tt.Image().RGBAAt(x, y); pixel.R > 180 && pixel.G < 100 {
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("the downloaded cover never appeared in the card")
		case <-tick.C:
			tt.Frame()
		}
	}
}

func TestReaderControlsToggle(t *testing.T) {
	a := app{
		phone: true, statePath: filepath.Join(t.TempDir(), "library.json"),
		comic:    item{Title: "试读", URL: "comic"},
		chapters: []item{{Title: "第一章", URL: "chapter-1"}},
	}
	a.showChapter(0, make([]item, 4), 0)
	tt := ui.NewTester(a.view, 360, 780)
	if tt.HasText("章节列表") || tt.HasText("设置") || tt.HasText("1 / 4 页") {
		t.Fatalf("reader controls appeared before tapping: %v", tt.Texts())
	}
	tt.FingerDown(180, 580)
	tt.FingerMove(180, 510)
	tt.FingerMove(180, 300)
	tt.Advance(250 * time.Millisecond)
	tt.FingerUp(180, 300)
	if a.readerControls {
		t.Fatal("vertical swipe revealed the controls")
	}
	first, last := a.reader.Visible()
	tap := func() {
		tt.FingerDown(180, 350)
		tt.FingerUp(180, 350)
	}
	tap()
	if !tt.HasText("章节列表") || !tt.HasText("设置") {
		t.Fatalf("tap did not reveal the controls: %v", tt.Texts())
	}
	if from, to := a.reader.Visible(); from != first || to != last {
		t.Fatalf("revealing the controls moved the viewport from %d–%d to %d–%d", first, last, from, to)
	}
	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("界面模式") || !tt.HasText("跟随系统") {
		t.Fatalf("appearance options are missing: %v", tt.Texts())
	}
	if err := tt.Click("夜晚"); err != nil {
		t.Fatal(err)
	}
	if a.library.Appearance != mygo.ThemeDark || !a.readerControls {
		t.Fatal("changing appearance should keep the reader controls open")
	}
	tap()
	if a.readerControls || a.readerSettings || tt.HasText("设置") {
		t.Fatalf("tapping the page did not close the controls: %v", tt.Texts())
	}
	tap()
	tt.Key(0, ui.KeyBack)
	if a.readerControls || len(a.pages) == 0 {
		t.Fatal("Back should close the controls before leaving the reader")
	}
}

func TestReaderPageSlider(t *testing.T) {
	for _, device := range []struct {
		name  string
		width int
		phone bool
	}{{"desktop", 1040, false}, {"phone", 360, true}} {
		t.Run(device.name, func(t *testing.T) {
			a := app{
				phone:     device.phone,
				statePath: filepath.Join(t.TempDir(), "library.json"),
				chapters:  []item{{Title: "第一章", URL: "chapter-1"}, {Title: "第二章", URL: "chapter-2"}},
			}
			a.showChapter(0, make([]item, 5), 0)
			for i := range a.pageRatios {
				a.pageRatios[i] = 0.2 // Short pages expose jumps clamped at the end of the list.
			}
			var opened []int
			tt := ui.NewTester(func(c *ui.Context) {
				ui.Column(c).Fill().Children(func() {
					a.readView(c, device.phone, func(index int) { opened = append(opened, index) })
				})
			}, device.width, 780)
			tapPage := func() {
				x := float32(device.width) / 4
				if device.phone {
					tt.FingerDown(x, 350)
					tt.FingerUp(x, 350)
				} else {
					tt.ClickAt(x, 350)
				}
			}
			tapPage()
			slider, ok := tt.AccessByRole(ui.RoleSlider, "跳页")
			if !ok {
				t.Fatal("reader progress slider is missing")
			}
			x, y := slider.Bounds.X+2, slider.Bounds.Y+slider.Bounds.H/2
			end := slider.Bounds.X + slider.Bounds.W - 2
			press, move, release, cancel := tt.Press, tt.Move, tt.Release, tt.CancelPress
			if device.phone {
				press, move, release, cancel = tt.FingerDown, tt.FingerMove, tt.FingerUp, tt.FingerCancel
			}
			press(x, y)
			move(end, y)
			if !tt.HasText("5 / 5 页") || a.firstPage != 0 {
				t.Fatalf("drag should preview page 5 while page %d is open, text = %v", a.firstPage+1, tt.Texts())
			}
			cancel()
			if !tt.HasText("1 / 5 页") || a.firstPage != 0 {
				t.Fatalf("canceled drag changed the page to %d, text = %v", a.firstPage+1, tt.Texts())
			}
			press(x, y)
			move(end, y)
			release(end, y)
			first, _ := a.reader.Visible()
			if first != 4 || a.firstPage != 4 || !tt.HasText("5 / 5 页") || len(opened) != 0 {
				t.Fatalf("jump landed on page %d, visible page %d, opened chapters %v", a.firstPage+1, first+1, opened)
			}
			a.remember()
			library, err := readLibrary(a.statePath)
			if err != nil || library.Last == nil || library.Last.Page != 4 {
				t.Fatalf("jump progress was not saved: %v, %v", library.Last, err)
			}
			tt.Scroll(float32(device.width)/2, 350, 0, -3000)
			if a.firstPage != 0 || !tt.HasText("1 / 5 页") {
				t.Fatalf("slider did not follow scrolling: page %d, text = %v", a.firstPage+1, tt.Texts())
			}
			press(end, y)
			release(end, y)
			if a.firstPage != 4 {
				t.Fatalf("tap did not jump to the final page: page %d", a.firstPage+1)
			}
			if !device.phone {
				tt.Key(0, ui.KeyLeft)
				if a.firstPage != 3 || !tt.HasText("4 / 5 页") {
					t.Fatalf("left arrow did not jump one page: page %d", a.firstPage+1)
				}
			}
			a.showChapter(1, []item{{}}, 0)
			tt.Frame()
			if a.readerControls {
				t.Fatal("controls stayed open in the next chapter")
			}
			tapPage()
			if _, ok := tt.AccessByRole(ui.RoleSlider, "跳页"); ok || !tt.HasText("1 / 1 页") {
				t.Fatalf("single-page chapter did not reset the progress display: %v", tt.Texts())
			}
		})
	}
}

func TestPhoneContinuesOnlyAfterScrollingPastImage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if err := png.Encode(w, image.NewGray(image.Rect(0, 0, 80, 400))); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	defer netimage.Forget(server.URL)

	a := app{
		phone:     true,
		statePath: filepath.Join(t.TempDir(), "library.json"),
		chapters:  []item{{Title: "第一章", URL: server.URL + "/1"}, {Title: "第二章", URL: server.URL + "/2"}},
	}
	a.showChapter(0, []item{{URL: server.URL}}, 0)
	var opened []int
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() {
			a.readView(c, true, func(index int) { opened = append(opened, index) })
		})
	}, 360, 780)
	deadline := time.NewTimer(time.Minute)
	defer deadline.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for !a.lastPageReady {
		select {
		case <-deadline.C:
			t.Fatal("page image did not load")
		case <-tick.C:
			tt.Frame()
		}
	}
	tt.Scroll(180, 500, 0, 200)
	if len(opened) != 0 {
		t.Fatalf("continued before reaching the bottom of the last image: %v", opened)
	}
	tt.Scroll(180, 500, 0, 2000)
	if !slices.Equal(opened, []int{1}) {
		t.Fatalf("scrolling past the last image opened chapters %v, want [1]", opened)
	}
	tt.Scroll(180, 500, 0, -2000)
	tt.Scroll(180, 500, 0, 2000)
	if !slices.Equal(opened, []int{1}) {
		t.Fatalf("the reader requested the next chapter twice: %v", opened)
	}
}

func TestPhoneReaderKeepsNaturalSwipeDistance(t *testing.T) {
	const pageCount = 96
	swipe := func(step time.Duration) (dragPage, finalPage int) {
		a := app{
			phone:     true,
			statePath: filepath.Join(t.TempDir(), "library.json"),
			chapters:  []item{{Title: "长章节", URL: "https://example.test/chapter"}},
		}
		a.showChapter(0, make([]item, pageCount), 0)
		for i := range a.pageRatios {
			a.pageRatios[i] = 0.8 + float32(i%6)*0.5
		}
		tt := ui.NewTester(func(c *ui.Context) {
			ui.Column(c).Fill().Children(func() {
				a.readView(c, true, func(int) {})
			})
		}, 360, 780)
		tt.FingerDown(180, 650)
		for _, y := range []float32{530, 410, 290, 170} {
			tt.Advance(step)
			tt.FingerMove(180, y)
		}
		dragPage, _ = a.reader.Visible()
		tt.FingerUp(180, 170)
		tt.Advance(4 * time.Second)
		finalPage, _ = a.reader.Visible()
		return
	}
	slowDrag, slowEnd := swipe(70 * time.Millisecond)
	fastDrag, fastEnd := swipe(12 * time.Millisecond)
	if fastDrag != slowDrag {
		t.Fatalf("drag moved to page %d when slow and %d when fast before lift", slowDrag+1, fastDrag+1)
	}
	if slowEnd < slowDrag || fastEnd <= slowEnd || fastEnd >= pageCount-1 {
		t.Fatalf("slow swipe reached page %d, fast swipe reached page %d in a %d-page chapter", slowEnd+1, fastEnd+1, pageCount)
	}
	t.Logf("slow swipe reached page %d, fast swipe reached page %d", slowEnd+1, fastEnd+1)
}

func TestCimocImportReport(t *testing.T) {
	a := app{
		chapter:      -1,
		sourcePicker: true,
		library: library{CimocReport: []sourceSkip{{
			ID: "WEBTOON", Reason: "只提供地址或站点参数，需要对应的解析器", Website: "https://m.webtoons.com",
		}}},
	}
	tt := ui.NewTester(a.view, 360, 780)
	if err := tt.Click("查看导入明细"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("WEBTOON") || !tt.HasText("需要对应的解析器") || !tt.HasText("打开原站") {
		t.Fatalf("skipped source diagnostic not visible: %v", tt.Texts())
	}
	tt.Key(0, ui.KeyBack)
	if !tt.HasText("图源配置 URL") || tt.HasText("WEBTOON") {
		t.Fatalf("back did not restore source picker: %v", tt.Texts())
	}
}
