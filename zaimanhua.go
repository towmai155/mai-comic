package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const zaimanhuaHome = "https://manhua.zaimanhua.com"

var zaimanhuaSlug = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func isZaimanhuaHome(address *url.URL) bool {
	if (address.Path != "" && address.Path != "/") || address.RawQuery != "" || address.Fragment != "" {
		return false
	}
	_, ok := findCimocAdapter("HAOMAN8", address.String())
	return ok
}

func searchZaimanhua(ctx context.Context, client *http.Client, query string) ([]item, error) {
	var data struct {
		List []struct {
			Title string `json:"title"`
			Slug  string `json:"comic_py"`
			Cover string `json:"cover"`
		} `json:"list"`
	}
	params := url.Values{"keyword": {query}, "source": {"0"}, "page": {"1"}, "size": {"30"}}
	if err := fetchZaimanhua(ctx, client, "/app/v1/search/index", params, &data); err != nil {
		return nil, err
	}
	results := make([]item, 0, len(data.List))
	base, _ := url.Parse(zaimanhuaHome)
	for _, comic := range data.List {
		if comic.Title != "" && zaimanhuaSlug.MatchString(comic.Slug) {
			results = append(results, item{Title: comic.Title, URL: zaimanhuaHome + "/info/" + comic.Slug + ".html",
				Cover: coverURL(base, comic.Cover)})
		}
	}
	return results, nil
}

func loadZaimanhuaComic(ctx context.Context, client *http.Client, comic item) (item, []item, error) {
	address, err := resolveURL(nil, comic.URL)
	if err != nil || address.Host != "manhua.zaimanhua.com" {
		return item{}, nil, errors.New("再漫画的作品链接无效")
	}
	slug, ok := strings.CutPrefix(address.Path, "/info/")
	slug, suffix := strings.CutSuffix(slug, ".html")
	if !ok || !suffix || !zaimanhuaSlug.MatchString(slug) {
		return item{}, nil, errors.New("再漫画的作品链接无效")
	}
	var data struct {
		Comic struct {
			ID          int64  `json:"id"`
			Title       string `json:"title"`
			Description string `json:"description"`
			Cover       string `json:"cover"`
			Groups      []struct {
				Chapters []struct {
					ID    int64  `json:"chapter_id"`
					Title string `json:"chapter_title"`
				} `json:"data"`
			} `json:"chapterList"`
		} `json:"comicInfo"`
	}
	params := url.Values{"comic_py": {slug}}
	if err := fetchZaimanhua(ctx, client, "/api/v1/comic2/comic/detail", params, &data); err != nil {
		return item{}, nil, err
	}
	info := data.Comic
	if info.ID <= 0 || info.Title == "" {
		return item{}, nil, errors.New("再漫画没有返回作品详情")
	}
	var chapters []item
	for _, group := range info.Groups {
		for _, chapter := range group.Chapters {
			if chapter.ID > 0 && chapter.Title != "" {
				address := fmt.Sprintf("%s/view/%s/%d/%d", zaimanhuaHome, slug, info.ID, chapter.ID)
				chapters = append(chapters, item{Title: chapter.Title, URL: address})
			}
		}
	}
	return item{Title: info.Title, URL: comic.URL, Summary: info.Description,
		Cover: coverURL(address, info.Cover)}, chapters, nil
}

func parseZaimanhuaPages(ctx context.Context, client *http.Client, chapter *url.URL) ([]item, error) {
	parts := strings.Split(strings.TrimPrefix(chapter.Path, "/"), "/")
	if chapter.Host != "manhua.zaimanhua.com" || len(parts) != 4 || parts[0] != "view" || !zaimanhuaSlug.MatchString(parts[1]) {
		return nil, errors.New("再漫画的章节链接无效")
	}
	comicID, comicErr := strconv.ParseInt(parts[2], 10, 64)
	chapterID, chapterErr := strconv.ParseInt(parts[3], 10, 64)
	if comicErr != nil || chapterErr != nil || comicID <= 0 || chapterID <= 0 {
		return nil, errors.New("再漫画的章节链接无效")
	}
	var data struct {
		Chapter struct {
			Pages []string `json:"page_url"`
		} `json:"chapterInfo"`
	}
	params := url.Values{"comic_id": {parts[2]}, "chapter_id": {parts[3]}}
	if err := fetchZaimanhua(ctx, client, "/api/v1/comic2/chapter/detail", params, &data); err != nil {
		return nil, err
	}
	pages := make([]item, 0, len(data.Chapter.Pages))
	for _, page := range data.Chapter.Pages {
		address, err := resolveURL(nil, page)
		if err != nil {
			return nil, fmt.Errorf("再漫画第 %d 张图片的链接无效：%w", len(pages)+1, err)
		}
		pages = append(pages, item{URL: address.String()})
	}
	return pages, nil
}

func fetchZaimanhua[T any](ctx context.Context, client *http.Client, path string, params url.Values, data *T) error {
	params.Set("channel", "pc")
	params.Set("app_name", "zmh")
	params.Set("version", "1.0.0")
	params.Set("uid", "0")
	address, err := url.Parse(zaimanhuaHome + path + "?" + params.Encode())
	if err != nil {
		return err
	}
	body, _, err := fetch(ctx, client, address, 8<<20)
	if err != nil {
		return err
	}
	var result struct {
		Code    int    `json:"errno"`
		Message string `json:"errmsg"`
		Data    *T     `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("再漫画返回的内容无法解析：%w", err)
	}
	if result.Code != 0 {
		return fmt.Errorf("再漫画返回错误 %d：%s", result.Code, result.Message)
	}
	if result.Data == nil {
		return errors.New("再漫画没有返回数据")
	}
	*data = *result.Data
	return nil
}
