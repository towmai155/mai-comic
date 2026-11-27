package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

const mkzhanReaderAPI = "https://comic.mkzcdn.com"

var mkzhanChapterPath = regexp.MustCompile("^/([0-9]+)/([0-9]+)\\.html$")

func parseMKZhanPages(ctx context.Context, client *http.Client, chapter *url.URL) ([]item, error) {
	path := mkzhanChapterPath.FindStringSubmatch(chapter.Path)
	if chapter.Scheme != "https" || len(path) == 0 {
		return nil, errors.New("漫客栈章节地址格式无效")
	}
	switch chapter.Host {
	case "www.mkzhan.com", "m.mkzhan.com", "mkzhan.com":
	default:
		return nil, errors.New("漫客栈章节地址不属于原站")
	}
	params := url.Values{"comic_id": {path[1]}, "chapter_id": {path[2]}}
	referer := "https://" + chapter.Host + "/"
	var info struct {
		Status json.Number
		Price  json.Number
		VIP    json.Number `json:"is_vip"`
	}
	if err := fetchMKZhanChapter(ctx, client, "/chapter/info/", params, referer, &info); err != nil {
		return nil, err
	}
	if info.Status != "1" || info.Price != "0" || info.VIP != "0" {
		return nil, errors.New("漫客栈未开放本章的公开读图，请在原站打开")
	}

	params.Set("quality", "1")
	params.Set("type", "1")
	params.Set("format", "0")
	params.Set("uid", "")
	params.Set("sign", "")
	var content struct {
		Page []struct {
			Image string
		}
	}
	if err := fetchMKZhanChapter(ctx, client, "/chapter/content/v1/", params, referer, &content); err != nil {
		return nil, err
	}
	pages := make([]item, 0, len(content.Page))
	for _, page := range content.Page {
		address, err := resolveURL(chapter, page.Image)
		if err != nil || !strings.HasSuffix(address.Hostname(), ".mkzcdn.com") {
			return nil, errors.New("漫客栈返回了无效的图片地址")
		}
		address.Scheme = "https"
		pages = append(pages, item{URL: address.String()})
	}
	return pages, nil
}

func fetchMKZhanChapter[T any](ctx context.Context, client *http.Client, path string, params url.Values, referer string, data *T) error {
	target, err := url.Parse(mkzhanReaderAPI + path + "?" + params.Encode())
	if err != nil {
		return err
	}
	body, final, err := fetchWithReferer(ctx, client, target, 2<<20, referer)
	if err != nil {
		return err
	}
	var result struct {
		Code json.Number
		Data *T
	}
	if final.Scheme != target.Scheme || final.Host != target.Host || json.Unmarshal(body, &result) != nil {
		return errors.New("漫客栈返回的章节数据格式无效")
	}
	if result.Code != "200" || result.Data == nil {
		return errors.New("漫客栈没有提供本章的公开读图数据，请在原站打开")
	}
	*data = *result.Data
	return nil
}
