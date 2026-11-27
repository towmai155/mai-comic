package main

import (
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestCopyMangaRankFetchesMaleBoardsInSiteOrder(t *testing.T) {
	const ranking = `<ul class="ranking-all"><li><div class="ranking-all-box">
		<img data-src="/cover-1.jpg"><div class="ranking-all-topThree-txt"><a href="/comic/one">榜首</a></div>
		</div></li><li><div class="ranking-all-box">
		<img data-src="/cover-2.jpg"><div class="ranking-all-topThree-txt"><a href="/comic/two">第二名</a></div>
		</div></li></ul><a href="/comic/outside">推荐作品</a>`
	for _, period := range copyMangaRankPeriods {
		t.Run(period.slug, func(t *testing.T) {
			client := &http.Client{Transport: cimocTestTransport(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path != "/rank" || req.URL.Query().Get("type") != "male" || req.URL.Query().Get("table") != period.slug {
					t.Fatalf("unexpected ranking request: %s", req.URL)
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(ranking)), Request: req}, nil
			})}
			items, err := fetchCopyMangaRank(t.Context(), client, period.slug)
			if err != nil || len(items) != 2 || items[0].Title != "榜首" || items[1].URL != copyMangaOrigin+"/comic/two" ||
				items[0].Cover != copyMangaOrigin+"/cover-1.jpg" {
				t.Fatalf("items=%+v error=%v", items, err)
			}
		})
	}
	client := &http.Client{Transport: cimocTestTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("type") == "male" {
			return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"/rank?type=female&table=day"}}, Body: http.NoBody, Request: req}, nil
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(ranking)), Request: req}, nil
	})}
	if items, err := fetchCopyMangaRank(t.Context(), client, "day"); err == nil || len(items) != 0 {
		t.Fatalf("ranking accepted a response for a different channel: %v, %v", items, err)
	}
}

func TestPhoneCopyMangaRankingKeepsPlaceInSeparateTab(t *testing.T) {
	src, _ := adaptCimocSource("COPYMH", copyMangaOrigin)
	a := app{
		phone: true, chapter: -1, statePath: filepath.Join(t.TempDir(), "library.json"),
		library: library{Sources: []source{src}},
	}
	for period := range 2 {
		for i := range 25 {
			comic := item{Title: fmt.Sprintf("榜单 %d 作品 %d", period, i), URL: fmt.Sprintf("%s/comic/%d", copyMangaOrigin, i)}
			a.copyRank.boards[period].items = append(a.copyRank.boards[period].items, searchMatch{Item: comic, SourceID: src.ID, SourceName: src.Name})
		}
	}
	tt := ui.NewTester(a.view, 320, 560)
	click := func(label string) {
		t.Helper()
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	if !tt.HasText("找漫画") || tt.HasText("拷贝漫画男频榜") {
		t.Fatalf("expected ranking in the navigation, not in the search panel: %v", tt.Texts())
	}
	click("男频榜")
	for _, label := range []string{"拷贝漫画男频排行榜", "日榜", "周榜", "月榜", "总榜", "榜单 0 作品 0"} {
		if !tt.HasText(label) {
			t.Fatalf("missing %q on ranking page: %v", label, tt.Texts())
		}
	}
	if tt.HasText("女频") || tt.HasText("搜索漫画") {
		t.Fatalf("unexpected controls on ranking page: %v", tt.Texts())
	}
	day := &a.copyRank.boards[0]
	tt.Scroll(160, 400, 0, 650)
	first, _ := day.scroll.Visible()
	if first <= 0 {
		t.Fatal("the daily list did not scroll")
	}
	a.comic, a.detail, a.comicSource = day.items[first].Item, day.items[first].Item, src.ID
	tt.Frame()
	click("返回榜单")
	if got, _ := day.scroll.Visible(); got != first {
		t.Fatalf("after leaving details first visible work = %d, want %d", got, first)
	}
	click("周榜")
	if !tt.HasText("榜单 1 作品 0") {
		t.Fatalf("weekly list did not start at its own position: %v", tt.Texts())
	}
	click("日榜")
	click("找漫画")
	click("男频榜")
	if got, _ := day.scroll.Visible(); got != first || !tt.HasText(day.items[first].Item.Title) {
		t.Fatalf("returning through the tabs reset the daily list: position=%d, want %d", got, first)
	}
}
