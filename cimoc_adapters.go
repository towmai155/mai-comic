package main

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

type chapterParser func(context.Context, *http.Client, *url.URL) ([]item, error)

type cimocAdapter struct {
	hosts               []string
	rules               source
	previousSearchItems string
	parsePages          chapterParser
	search              func(context.Context, *http.Client, string) ([]item, error)
	loadComic           func(context.Context, *http.Client, item) (item, []item, error)
}

var cimocAdapters = map[string]cimocAdapter{
	"COPYMH": {
		hosts:      []string{"www.copy20.com", "copy20.com", "www.copy4000.com", "copy4000.com"},
		search:     searchCopyManga,
		loadComic:  loadCopyMangaComic,
		parsePages: parseCopyMangaPages,
		rules:      source{Name: "拷贝漫画", BaseURL: copyMangaOrigin},
	},
	"JMTT": {
		hosts:      []string{jmttHost, jmttNewHost, jmttFufiHost},
		parsePages: parseJMTTPages,
		rules: source{
			Name: "禁漫天堂",
			Search: rule{Format: "html", URL: "/search/photos?search_query={query}",
				Items:  "#wrapper .list-col:has(.thumb-overlay a[href^='/album/']):has(.video-title)",
				Fields: map[string]string{"title": ".video-title", "url": ".thumb-overlay a[href^='/album/']@href", "cover": ".thumb-overlay img@src"}},
			Details: rule{Format: "html", Fields: map[string]string{"title": "h1#book-name", "cover": "#album_photo img@src"}},
			Chapters: rule{Format: "html",
				Items:  "#episode-block a[data-album][href^='/photo/'], body:not(:has(#episode-block a[data-album][href^='/photo/'])) a.reading[href^='/photo/']",
				Fields: map[string]string{"title": ".", "url": "@href"}},
		},
	},
	"DASHUMANHUA": {
		hosts:               []string{"www.dashumanhua.com", "dashumanhua.com"},
		previousSearchItems: "dl.alllist dt .ti a[href]",
		parsePages:          parseDashuPages,
		rules: source{
			Name: "大树漫画",
			Search: rule{Format: "html", URL: "/search?types=comic&searhword={query}", Items: "dl.alllist dt:has(.ti a[href])",
				Fields: map[string]string{"title": ".ti a[href]", "url": ".ti a[href]@href"}},
			Details: rule{Format: "html",
				Fields: map[string]string{"title": "h1.bookname"}},
			Chapters: rule{Format: "html", Items: "ul#ul_chapter1 a[href*='/read-']",
				Fields: map[string]string{"title": ".", "url": "@href"}},
		},
	},
	"DM5": {
		hosts:      []string{"m.dm5.com"},
		parsePages: parseDM5Pages,
		rules: source{
			Name: "动漫屋",
			Search: rule{Format: "html", URL: "/search?title={query}", Items: "ul.book-list .book-list-cover a[href][title]",
				Fields: map[string]string{"title": "@title", "url": "@href", "cover": "img@src"}},
			Details: rule{Format: "html",
				Fields: map[string]string{"title": ".detail-main-info-title", "summary": "p.detail-desc"}},
			Chapters: rule{Format: "html", Items: ".detail-list-select a.chapteritem[href]",
				Fields: map[string]string{"title": ".detail-list-2-info-title", "url": "@href"}},
		},
	},
	"DMZJFIX": {
		hosts:      []string{"www.wmh1234.com", "wmh1234.com"},
		parsePages: parseManga1234Pages,
		rules: source{
			Name: "漫画1234",
			Search: rule{Format: "html", URL: "/index.php/search?key={query}", Items: ".search-results a.comic-card[href][title]",
				Fields: map[string]string{"title": "@title", "url": "@href", "cover": "img@src"}},
			Details: rule{Format: "html",
				Fields: map[string]string{"title": "h1[data-comic-name]", "summary": ".mint-detail-description"}},
			Chapters: rule{Format: "html", Items: ".mint-chapters a[href]",
				Fields: map[string]string{"title": ".", "url": "@href"}},
		},
	},
	"DUMANWUORG": {
		hosts:               []string{"www.dumanwu.org", "dumanwu.org"},
		previousSearchItems: ".bookList_2 .item .title a[href]",
		parsePages:          parseDumanwuPages,
		rules: source{
			Name: "读漫屋 org",
			Search: rule{Format: "html", URL: "/search?key={query}", Items: ".bookList_2 .item:has(.title a[href])",
				Fields: map[string]string{"title": ".title a[href]", "url": ".title a[href]@href"}},
			Details: rule{Format: "html",
				Fields: map[string]string{"title": ".ib.info:has(.grading) p.title"}},
			Chapters: rule{Format: "html", Items: "#chapterlistload .list a[href$='.html']",
				Fields: map[string]string{"title": ".", "url": "@href"}},
		},
	},
	"HAOMAN8": {
		hosts:      []string{"zaimanhua.com", "m.zaimanhua.com", "www.zaimanhua.com", "manhua.zaimanhua.com"},
		rules:      source{Name: "再漫画", BaseURL: zaimanhuaHome},
		search:     searchZaimanhua,
		loadComic:  loadZaimanhuaComic,
		parsePages: parseZaimanhuaPages,
	},
	"MANHUATAI": {
		hosts:      []string{"www.kanman.com", "m.kanman.com", "kanman.com", "www.manhuatai.com", "m.manhuatai.com", "manhuatai.com"},
		parsePages: parseKanmanPages,
		rules: source{
			Name: "漫画台（看漫画）", BaseURL: "https://m.kanman.com", BrowserReader: true,
			Search: rule{Format: "html", URL: "/sort/?search_key={query}", Items: "#js_comicSortList .comic-item > a[href]",
				Fields: map[string]string{"title": ".title", "url": "@href"}},
			Details: rule{Format: "html", Fields: map[string]string{"title": "h1.comic-title", "summary": "#js_desc_content"}},
			Chapters: rule{Format: "html", Items: "#js_chapter_list a.chapter-name[href][title]",
				Fields: map[string]string{"title": "@title", "url": "@href"}},
		},
	},
	"MANMANMANHUA": {
		hosts:               []string{"www.manmanapp.com", "manmanapp.com"},
		previousSearchItems: ".mainBody .classification li.title a[href^='/comic-']",
		rules: source{
			Name: "漫漫漫画",
			Search: rule{Format: "html", URL: "/search/index.html?keyword={query}", Items: ".mainBody .classification:has(li.title a[href^='/comic-'])",
				Fields: map[string]string{"title": "li.title a[href^='/comic-']", "url": "li.title a[href^='/comic-']@href"}},
			Details: rule{Format: "html",
				Fields: map[string]string{"title": ".cartoon li.title", "summary": ".cartoon li.describe"}},
			Chapters: rule{Format: "html",
				Items:  ".cartoon a.start_read[href^='/comic/detail-'], .catalog .comic_list li a[href^='/comic/detail-']",
				Fields: map[string]string{"title": "h3", "fallbackTitle": ".", "url": "@href"}},
			Pages: rule{Format: "html", Items: "img.man_img[src]",
				Fields: map[string]string{"url": "@src"}},
		},
	},
	"MANHUAWU": {
		hosts: []string{"www.mhua5.com", "mhua5.com"},
		rules: source{
			Name: "漫画屋",
			Search: rule{Format: "html", URL: "/search/{query}/1", Items: ".search-comic-list .common-comic-item",
				Fields: map[string]string{"title": ".comic__title a", "url": ".comic__title a@href"}},
			Details: rule{Format: "html",
				Fields: map[string]string{"title": ".de-info__box .j-comic-title", "summary": ".comic-intro .intro-total"}},
			Chapters: rule{Format: "html", Items: ".chapter__list a.j-chapter-link[href]",
				Fields: map[string]string{"title": ".", "url": "@href"}},
			Pages: rule{Format: "html", Items: ".rd-article__pic img.lazy-read[data-original]",
				Fields: map[string]string{"url": "@data-original"}},
		},
	},
	"MKZHAN": {
		hosts:               []string{"comic.mkzhan.com", "www.mkzhan.com", "m.mkzhan.com", "mkzhan.com"},
		previousSearchItems: ".search-comic-list .comic__title a[href]",
		parsePages:          parseMKZhanPages,
		rules: source{
			Name: "漫客栈", BaseURL: "https://www.mkzhan.com", BrowserReader: true,
			Search: rule{Format: "html", URL: "/search?keyword={query}", Items: ".search-comic-list .common-comic-item:has(.comic__title a[href])",
				Fields: map[string]string{"title": ".comic__title a[href]", "url": ".comic__title a[href]@href"}},
			Details: rule{Format: "html", Fields: map[string]string{"title": ".de-info__box .j-comic-title", "summary": ".comic-intro .intro-total"}},
			Chapters: rule{Format: "html", Items: ".chapter__list a.j-chapter-link[data-hreflink]",
				Fields: map[string]string{"title": ".", "url": "@data-hreflink"}},
		},
	},
	"TENCENT": {
		hosts:      []string{"ac.qq.com", "m.ac.qq.com"},
		parsePages: parseTencentPages,
		rules: source{
			Name: "腾讯动漫", BaseURL: "https://ac.qq.com",
			Search: rule{Format: "html", URL: "/Comic/searchList/search/{query}", Items: ".mod_all_works_list a.mod_book_cover[href][title]",
				Fields: map[string]string{"title": "@title", "url": "@href"}},
			Details: rule{Format: "html",
				Fields: map[string]string{"title": "h1.works-intro-title", "summary": "p.works-intro-short"}},
			Chapters: rule{Format: "html", Items: ".chapter-page-all .works-chapter-item a[href^='/ComicView/']",
				Fields: map[string]string{"title": ".", "url": "@href"}},
		},
	},
	"WEBTOON": {
		hosts: []string{"www.webtoons.com", "m.webtoons.com", "webtoons.com"},
		rules: source{
			Name: "WEBTOON 英文", BaseURL: "https://www.webtoons.com", BrowserReader: true,
			Search: rule{Format: "html", URL: "/en/search?keyword={query}", Items: "a._card_item[href]",
				Fields: map[string]string{"title": "strong.title", "url": "@href", "cover": "img@src"}},
			Details: rule{Format: "html", Fields: map[string]string{"title": "h1.subj", "summary": "p.summary"}},
			Chapters: rule{Format: "html", Items: "#_listUl a.detail_list_link[href*='/viewer?']",
				Next:   ".paginate a.pg_page[aria-current='true'] + a[href]:not([href='#'])@href",
				Fields: map[string]string{"title": ".subj", "url": "@href"}},
		},
	},
	"WEBTOONDONGMANMANHUA": {
		hosts: []string{"www.dongmanmanhua.cn", "dongmanmanhua.cn"},
		rules: source{
			Name: "咚漫漫画",
			Search: rule{Format: "html", URL: "/search?keyword={query}", Items: ".card_lst li a.card_item[href]",
				Fields: map[string]string{"title": ".subj", "url": "@href", "cover": "img@src"}},
			Details: rule{Format: "html",
				Fields: map[string]string{"title": "h1.subj", "summary": "p.summary"}},
			Chapters: rule{Format: "html", Items: "#_listUl li a[href*='/viewer?']",
				Next:   ".paginate a:has(span.on) + a[href]:not([href='#'])@href",
				Fields: map[string]string{"title": ".subj", "url": "@href"}},
			Pages: rule{Format: "html", Items: "#_imageList img[data-url]",
				Fields: map[string]string{"url": "@data-url"}},
		},
	},
}

func adaptCimocSource(id, address string) (source, bool) {
	switch id {
	case "DongManWu":
		id = "DM5"
	case "ManHuaTai":
		id = "MANHUATAI"
	case "TENCENTM":
		id = "TENCENT"
	}
	adapter, ok := findCimocAdapter(id, address)
	if !ok {
		return source{}, false
	}
	src := adapter.rules
	src.ID = "cimoc-" + id
	if src.BaseURL == "" {
		src.BaseURL = address
	}
	return src, true
}

func findCimocAdapter(id, address string) (cimocAdapter, bool) {
	adapter, ok := cimocAdapters[id]
	if !ok {
		return cimocAdapter{}, false
	}
	base, err := resolveURL(nil, address)
	if err != nil {
		return cimocAdapter{}, false
	}
	for _, host := range adapter.hosts {
		if strings.EqualFold(base.Hostname(), host) {
			return adapter, true
		}
	}
	return cimocAdapter{}, false
}

func (s source) chapterParser() chapterParser {
	if s.Pages.Format != "" {
		return nil
	}
	return s.cimocAdapter().parsePages
}

func (s source) cimocAdapter() cimocAdapter {
	id, ok := strings.CutPrefix(s.ID, "cimoc-")
	if !ok {
		return cimocAdapter{}
	}
	adapter, _ := findCimocAdapter(id, s.BaseURL)
	return adapter
}

func (s source) readsInBrowser() bool {
	return s.BrowserReader && s.chapterParser() == nil
}
