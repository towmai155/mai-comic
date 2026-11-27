package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

const (
	copyMangaOrigin          = "https://www.copy4000.com"
	copyMangaAlternateOrigin = "https://www.copy20.com"
)

var (
	copyMangaSlug         = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	copyMangaChapterID    = regexp.MustCompile(`^[0-9a-fA-F]{8}(?:-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}$`)
	copyMangaDNT          = regexp.MustCompile(`<span\b[^>]*\bid="dnt"[^>]*\bvalue="([0-9]+)"`)
	copyMangaDirectoryKey = regexp.MustCompile(`\bvar\s+ccz\s*=\s*'([^']+)'`)
	copyMangaReaderKey    = regexp.MustCompile(`\bvar\s+cct\s*=\s*'([^']+)'`)
	copyMangaReaderData   = regexp.MustCompile(`\bvar\s+contentKey\s*=\s*'([^']*)'`)
	copyMangaDetails      = rule{Format: "html", Fields: map[string]string{
		"title": ".comicParticulars-title-right h6", "summary": ".comicParticulars-synopsis .intro",
		"cover": ".comicParticulars-left-img img@data-src",
	}}
)

func searchCopyManga(ctx context.Context, client *http.Client, query string) ([]item, error) {
	params := url.Values{"q": {query}, "q_type": {""}, "platform": {"2"}, "limit": {"12"}, "offset": {"0"}}
	body, final, err := fetchCopyMangaWeb(ctx, client, "/api/kb/web/searchcl/comics?"+params.Encode())
	if err != nil {
		return nil, err
	}
	return parseCopyMangaSearch(body, final)
}

func fetchCopyMangaWeb(ctx context.Context, client *http.Client, path string) ([]byte, *url.URL, error) {
	for _, origin := range []string{copyMangaOrigin, copyMangaAlternateOrigin} {
		target, _ := url.Parse(origin + path)
		body, final, err := fetch(ctx, client, target, 2<<20)
		if err == nil {
			if !copyMangaWebsite(final) {
				return nil, nil, errors.New("拷贝漫画请求跳转到了未知站点")
			}
			return body, final, nil
		}
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		var connectionError *url.Error
		if !errors.As(err, &connectionError) {
			return nil, nil, err
		}
	}
	return nil, nil, errors.New("连接失败，请切换网络后重试")
}

func parseCopyMangaSearch(body []byte, responseURL *url.URL) ([]item, error) {
	var response struct {
		Code    int
		Results struct {
			List []struct {
				Name  string
				Slug  string `json:"path_word"`
				Cover string
			}
		}
	}
	if json.Unmarshal(body, &response) != nil || response.Code != 200 {
		return nil, errors.New("拷贝漫画没有返回有效的搜索结果")
	}
	var results []item
	for _, comic := range response.Results.List {
		if comic.Name != "" && copyMangaSlug.MatchString(comic.Slug) {
			address := &url.URL{Scheme: responseURL.Scheme, Host: responseURL.Host, Path: "/comic/" + comic.Slug}
			results = append(results, item{Title: comic.Name, URL: address.String(), Cover: coverURL(responseURL, comic.Cover)})
		}
	}
	return results, nil
}

func loadCopyMangaComic(ctx context.Context, client *http.Client, comic item) (item, []item, error) {
	address, err := resolveURL(nil, comic.URL)
	if err != nil || !copyMangaWebsite(address) {
		return item{}, nil, errors.New("拷贝漫画作品地址无效")
	}
	slug, ok := strings.CutPrefix(strings.TrimSuffix(address.Path, "/"), "/comic/")
	if !ok || !copyMangaSlug.MatchString(slug) {
		return item{}, nil, errors.New("拷贝漫画作品地址无效")
	}
	body, final, err := fetch(ctx, client, address, 4<<20)
	if err != nil {
		return item{}, nil, err
	}
	if !copyMangaWebsite(final) {
		return item{}, nil, errors.New("拷贝漫画作品跳转到未知站点")
	}
	details, _, err := copyMangaDetails.parsePage(body, final, true)
	if err != nil {
		return item{}, nil, err
	}
	detail := details[0]
	detail.URL = address.String()
	key, dnt := copyMangaDirectoryKey.FindSubmatch(body), copyMangaDNT.FindSubmatch(body)
	if key == nil || dnt == nil {
		return item{}, nil, errors.New("拷贝漫画作品页缺少目录参数")
	}
	endpoint := &url.URL{Scheme: final.Scheme, Host: final.Host, Path: "/comicdetail/" + slug + "/chapters"}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return item{}, nil, err
	}
	request.Header.Set("dnts", string(dnt[1]))
	request.Header.Set("Referer", final.String())
	body, _, err = fetchRequest(client, request, 8<<20)
	if err != nil {
		return item{}, nil, err
	}
	var response struct {
		Code    int
		Results string
	}
	if json.Unmarshal(body, &response) != nil || response.Code != 200 {
		return item{}, nil, errors.New("拷贝漫画没有返回有效的章节目录")
	}
	var directory struct {
		Groups map[string]struct {
			Chapters []struct {
				ID   string
				Name string
			}
		}
	}
	if !decodeCopyManga(response.Results, key[1], &directory) {
		return item{}, nil, errors.New("拷贝漫画章节目录数据格式发生变化")
	}
	var chapters []item
	seen := make(map[string]bool)
	for _, group := range slices.Sorted(maps.Keys(directory.Groups)) {
		for _, chapter := range directory.Groups[group].Chapters {
			if chapter.Name == "" || !copyMangaChapterID.MatchString(chapter.ID) || seen[chapter.ID] {
				continue
			}
			seen[chapter.ID] = true
			link := &url.URL{Scheme: final.Scheme, Host: final.Host, Path: "/comic/" + slug + "/chapter/" + chapter.ID}
			chapters = append(chapters, item{Title: chapter.Name, URL: link.String()})
		}
	}
	if len(chapters) == 0 {
		return item{}, nil, errors.New("拷贝漫画未向当前连接提供章节目录")
	}
	return detail, chapters, nil
}

func parseCopyMangaPages(ctx context.Context, client *http.Client, chapter *url.URL) ([]item, error) {
	parts := strings.Split(strings.Trim(chapter.Path, "/"), "/")
	if !copyMangaWebsite(chapter) || len(parts) != 4 || parts[0] != "comic" || !copyMangaSlug.MatchString(parts[1]) ||
		parts[2] != "chapter" || !copyMangaChapterID.MatchString(parts[3]) {
		return nil, errors.New("拷贝漫画章节地址无效")
	}
	body, final, err := fetch(ctx, client, chapter, 8<<20)
	if err != nil {
		return nil, err
	}
	if !copyMangaWebsite(final) || strings.Trim(final.Path, "/") != strings.Join(parts, "/") {
		return nil, errors.New("拷贝漫画未返回所选章节的阅读页")
	}
	key, data := copyMangaReaderKey.FindSubmatch(body), copyMangaReaderData.FindSubmatch(body)
	if key == nil || data == nil || len(data[1]) == 0 {
		return nil, errors.New("拷贝漫画未向当前连接提供本章图片，请在原站确认")
	}
	var images []struct{ URL string }
	if !decodeCopyManga(string(data[1]), key[1], &images) {
		return nil, errors.New("拷贝漫画章节图片数据格式发生变化")
	}
	var pages []item
	for _, image := range images {
		if address := coverURL(final, image.URL); address != "" {
			pages = append(pages, item{URL: address})
		}
	}
	return pages, nil
}

func copyMangaWebsite(address *url.URL) bool {
	if address.Scheme != "https" {
		return false
	}
	switch strings.ToLower(address.Hostname()) {
	case "www.copy20.com", "copy20.com", "www.copy4000.com", "copy4000.com":
		return true
	default:
		return false
	}
}

// The public web reader prefixes its hex payload with a 16-byte IV.
func decodeCopyManga(payload string, key []byte, target any) bool {
	if len(payload) <= aes.BlockSize {
		return false
	}
	blk, err := aes.NewCipher(key)
	if err != nil {
		return false
	}
	data, err := hex.DecodeString(payload[aes.BlockSize:])
	if err != nil || len(data)%aes.BlockSize != 0 || len(data) == 0 {
		return false
	}
	cipher.NewCBCDecrypter(blk, []byte(payload[:aes.BlockSize])).CryptBlocks(data, data)
	padding := int(data[len(data)-1])
	if padding < 1 || padding > aes.BlockSize {
		return false
	}
	for _, value := range data[len(data)-padding:] {
		if int(value) != padding {
			return false
		}
	}
	return json.Unmarshal(data[:len(data)-padding], target) == nil
}
