package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
)

var (
	kanmanChapterPath = regexp.MustCompile(`^/[0-9]+/([a-zA-Z0-9_-]+)\.html$`)
	kanmanCurrent     = regexp.MustCompile(`\bcurrent_chapter\s*:\s*\{((?:"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|[^{}"'])*)\}`)
	kanmanChapterSlug = regexp.MustCompile(`(?:^|,)\s*chapter_newid\s*:\s*"([a-zA-Z0-9_-]+)"\s*(?:,|$)`)
	kanmanFreePrice   = regexp.MustCompile(`(?:^|,)\s*price\s*:\s*(?:0|"0")\s*(?:,|$)`)
	kanmanFreeVIP     = regexp.MustCompile(`(?:^|,)\s*is_vip\s*:\s*(?:0|"0")\s*(?:,|$)`)
	kanmanImages      = regexp.MustCompile(`(?:^|,)\s*chapter_img_list\s*:\s*`)
)

func parseKanmanPages(ctx context.Context, client *http.Client, chapter *url.URL) ([]item, error) {
	path := kanmanChapterPath.FindStringSubmatch(chapter.Path)
	if !kanmanReaderHost(chapter) || len(path) == 0 {
		return nil, errors.New("漫画台章节地址格式无效")
	}
	body, final, err := fetch(ctx, client, chapter, 8<<20)
	if err != nil {
		return nil, err
	}
	if !kanmanReaderHost(final) || final.Path != chapter.Path {
		return nil, errors.New("漫画台没有返回所选章节的公开阅读页")
	}
	_, script, ok := bytes.Cut(body, []byte("window.comicInfo="))
	if !ok {
		return nil, errors.New("漫画台阅读页缺少章节数据")
	}
	script, _, _ = bytes.Cut(script, []byte("</script>"))
	match := kanmanCurrent.FindSubmatch(script)
	if match == nil {
		return nil, errors.New("漫画台阅读页缺少当前章节数据")
	}
	current := match[1]
	slug := kanmanChapterSlug.FindSubmatch(current)
	if slug == nil || string(slug[1]) != path[1] {
		return nil, errors.New("漫画台返回的章节与所选章节不一致")
	}
	if !kanmanFreePrice.Match(current) || !kanmanFreeVIP.Match(current) {
		return nil, errors.New("漫画台未开放本章的公开读图，请在原站打开")
	}
	list := kanmanImages.FindIndex(current)
	if list == nil {
		return nil, errors.New("漫画台没有返回当前章节的图片列表")
	}
	var images []string
	if err := json.NewDecoder(bytes.NewReader(current[list[1]:])).Decode(&images); err != nil || len(images) == 0 {
		return nil, errors.New("漫画台图片列表为空或格式无效")
	}
	pages := make([]item, 0, len(images))
	for _, raw := range images {
		address, err := resolveURL(nil, raw)
		if err != nil || address.Scheme != "https" {
			return nil, errors.New("漫画台返回了无效的图片地址")
		}
		pages = append(pages, item{URL: address.String()})
	}
	return pages, nil
}

func kanmanReaderHost(address *url.URL) bool {
	return address.Scheme == "https" && (address.Host == "m.kanman.com" || address.Host == "www.kanman.com" || address.Host == "kanman.com")
}
