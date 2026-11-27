package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

const (
	cimocLegacyURL = "https://gitcode.net/Haleydutest/cupdate/-/raw/master/sourceBaseUrl.json"
	cimocRawURL    = "https://raw.gitcode.com/Haleydutest/cupdate/raw/main/sourceBaseUrl.json"
)

type cimocSource struct {
	Name             string `json:"name"`
	BaseURL          string `json:"baseUrl"`
	Search           string `json:"search"`
	SearchList       string `json:"searchInfoList"`
	SearchLink       string `json:"searchInfoCid"`
	SearchTitle      string `json:"searchInfoTitle"`
	DetailsTitle     string `json:"parseInfoTitle"`
	DetailsSummary   string `json:"parseInfoIntro"`
	ChapterList      string `json:"parseChapterList1"`
	ChapterList2     string `json:"parseChapterList2"`
	ChapterList3     string `json:"parseChapterList3"`
	ChapterLink      string `json:"parseChapterPath"`
	ChapterTitle     string `json:"parseChapterTitle"`
	ImageList        string `json:"parseImageList"`
	ImageAttr        string `json:"parseImageUrl"`
	ImageServer      string `json:"imageServerUrl"`
	ParseImageServer string `json:"parseImageServerUrl"`
}

func importCimoc(body []byte, knownURL bool) (sourceImport, bool) {
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(body, &entries); err != nil {
		return sourceImport{}, false
	}
	var result sourceImport
	isCimoc := knownURL
	seen := make(map[string]bool)
	for _, id := range slices.Sorted(maps.Keys(entries)) {
		var config cimocSource
		if err := json.Unmarshal(entries[id], &config); err != nil {
			config = cimocSource{}
			if json.Unmarshal(entries[id], &config.BaseURL) != nil {
				continue
			}
			if _, err := resolveURL(nil, config.BaseURL); err != nil {
				continue
			}
		}
		if config.BaseURL == "" {
			continue
		}
		src, adapted := adaptCimocSource(id, config.BaseURL)
		if adapted || (config.SearchList != "" && config.DetailsTitle != "") {
			isCimoc = true
		}
		var err error
		if !adapted {
			src, err = config.toSource(id)
		}
		if err == nil {
			err = src.validate()
		}
		if err != nil {
			result.Skipped = append(result.Skipped, sourceSkip{
				ID: id, Reason: err.Error(), Website: cimocWebsite(id, config.BaseURL),
			})
			continue
		}
		if seen[src.ID] {
			continue
		}
		seen[src.ID] = true
		result.Sources = append(result.Sources, src)
	}
	result.Cimoc = isCimoc
	return result, isCimoc
}

func (c cimocSource) toSource(id string) (source, error) {
	switch {
	case c.SearchList == "" && c.DetailsTitle == "" && c.ChapterList == "" && c.ImageList == "":
		return source{}, errors.New("只提供地址或站点参数，需要对应的解析器")
	case c.SearchList == "" || c.SearchLink == "":
		return source{}, errors.New("缺少可转换的搜索结果规则")
	case c.ImageServer != "" || c.ParseImageServer != "":
		return source{}, errors.New("图片服务器需要独立适配")
	case c.ImageList == "":
		return source{}, errors.New("缺少可转换的图片列表规则")
	}
	var chapterLists []string
	for _, list := range []string{c.ChapterList, c.ChapterList2, c.ChapterList3} {
		if list = strings.TrimSpace(list); list != "" {
			chapterLists = append(chapterLists, list)
		}
	}
	if len(chapterLists) == 0 {
		return source{}, errors.New("缺少章节列表规则")
	}

	imageAttr := c.ImageAttr
	switch imageAttr {
	case "":
		imageAttr = "src"
	case "src", "data-src", "data-original", "data-echo":
	default:
		return source{}, fmt.Errorf("图片字段 %q 需要独立适配", imageAttr)
	}
	title := c.ChapterTitle
	if title == "" {
		title = "."
	}
	name := strings.TrimSpace(c.Name)
	if name == "" {
		name = id
	}
	// Cimoc uses Jsoup's :eq(0) in some field selectors. Our field
	// selectors already take the first matching element in each row.
	searchLink := strings.TrimSuffix(c.SearchLink, ":eq(0)")
	searchTitle := strings.TrimSuffix(c.SearchTitle, ":eq(0)")
	searchURL := strings.NewReplacer("%s", "{query}", "%d", "1").Replace(c.Search)
	return source{
		ID: "cimoc-" + id, Name: name, BaseURL: c.BaseURL,
		Search: rule{Format: "html", URL: searchURL, Items: c.SearchList,
			Fields: map[string]string{"title": searchTitle, "url": searchLink + "@href"}},
		Details: rule{Format: "html",
			Fields: map[string]string{"title": c.DetailsTitle, "summary": c.DetailsSummary}},
		Chapters: rule{Format: "html", Items: strings.Join(chapterLists, ", "),
			Fields: map[string]string{"title": title, "url": c.ChapterLink + "@href"}},
		Pages: rule{Format: "html", Items: c.ImageList,
			Fields: map[string]string{"url": "@" + imageAttr}},
	}, nil
}
