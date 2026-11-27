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

const dm5Origin = "https://www.dm5.com/"

var (
	dm5ChapterPath = regexp.MustCompile(`^/m([0-9]+)/?$`)
	dm5Number      = regexp.MustCompile(`^[0-9]+$`)
	dm5Variables   = regexp.MustCompile(`\bvar\s+DM5_(MID|CID|IMAGE_COUNT|ISNEED|VIEWSIGN_DT|VIEWSIGN)\s*=\s*("(?:(?:\\.)|[^"\\])*"|[0-9]+|false|true)\s*;`)
	dm5ImageBase   = regexp.MustCompile(`\bvar\s+pix\s*=\s*("(?:\\.|[^"\\])*")`)
	dm5ImagePaths  = regexp.MustCompile(`\bvar\s+pvalue\s*=\s*(\[[^\]]*\])`)
	dm5ImageSuffix = regexp.MustCompile(`pvalue\[i\]\s*=\s*pix\s*\+\s*pvalue\[i\]\s*\+\s*'(\?[^']+)'`)
)

func parseDM5Pages(ctx context.Context, client *http.Client, chapter *url.URL) ([]item, error) {
	path := dm5ChapterPath.FindStringSubmatch(chapter.Path)
	if (chapter.Hostname() != "m.dm5.com" && chapter.Hostname() != "www.dm5.com") || len(path) == 0 {
		return nil, errors.New("动漫屋章节地址格式无效")
	}
	desktop := &url.URL{Scheme: "https", Host: "www.dm5.com", Path: "/m" + path[1] + "/"}
	body, final, err := fetch(ctx, client, desktop, 8<<20)
	if err != nil {
		return nil, err
	}
	if final.Hostname() != desktop.Hostname() {
		return nil, errors.New("动漫屋章节跳转到了未知站点")
	}

	values := make(map[string]string)
	for _, variable := range dm5Variables.FindAllStringSubmatch(string(body), -1) {
		value := variable[2]
		if strings.HasPrefix(value, `"`) {
			if err := json.Unmarshal([]byte(value), &value); err != nil {
				continue
			}
		}
		values[variable[1]] = value
	}
	if !strings.EqualFold(values["ISNEED"], "false") {
		return nil, errors.New("动漫屋未提供本章的公开读图数据，请在原站打开")
	}
	count, countErr := strconv.Atoi(values["IMAGE_COUNT"])
	cid, mid := values["CID"], values["MID"]
	if cid != path[1] || !dm5Number.MatchString(mid) || countErr != nil || count < 1 || count > 2000 ||
		values["VIEWSIGN_DT"] == "" || values["VIEWSIGN"] == "" {
		return nil, errors.New("动漫屋章节缺少读图参数")
	}

	endpoint := desktop.ResolveReference(&url.URL{Path: "chapterfun.ashx"})
	query := url.Values{
		"cid": {cid}, "_cid": {cid}, "_mid": {mid}, "language": {"1"}, "gtk": {"6"}, "key": {""},
		"_dt": {values["VIEWSIGN_DT"]}, "_sign": {values["VIEWSIGN"]},
	}
	pages := make([]item, 0, count)
	for len(pages) < count {
		query.Set("page", strconv.Itoa(len(pages)+1))
		endpoint.RawQuery = query.Encode()
		batchBody, batchURL, requestErr := fetchWithReferer(ctx, client, endpoint, 1<<20, desktop.String())
		if requestErr != nil {
			var urlErr *url.Error
			if errors.As(requestErr, &urlErr) {
				requestErr = urlErr.Err
			}
			return nil, fmt.Errorf("加载动漫屋第 %d 页：%w", len(pages)+1, requestErr)
		}
		if batchURL.Hostname() != desktop.Hostname() {
			return nil, errors.New("动漫屋图片列表跳转到了未知站点")
		}
		batch, key, parseErr := parseDM5Batch(string(batchBody), desktop)
		if parseErr != nil {
			return nil, fmt.Errorf("解析动漫屋第 %d 页：%w", len(pages)+1, parseErr)
		}
		if key != "" {
			query.Set("key", key)
		}
		pages = append(pages, batch[:min(len(batch), count-len(pages))]...)
	}
	return pages, nil
}

func parseDM5Batch(body string, chapter *url.URL) ([]item, string, error) {
	body, err := unpackReaderScript(body)
	if err != nil {
		return nil, "", err
	}
	base, paths := dm5ImageBase.FindStringSubmatch(body), dm5ImagePaths.FindStringSubmatch(body)
	if len(base) == 0 || len(paths) == 0 {
		return nil, "", errors.New("动漫屋响应没有图片列表")
	}
	var prefix string
	var files []string
	if json.Unmarshal([]byte(base[1]), &prefix) != nil || json.Unmarshal([]byte(paths[1]), &files) != nil || len(files) == 0 {
		return nil, "", errors.New("动漫屋图片列表格式无效")
	}
	var suffix, key string
	if match := dm5ImageSuffix.FindStringSubmatch(body); len(match) != 0 {
		suffix = match[1]
		if values, err := url.ParseQuery(strings.TrimPrefix(suffix, "?")); err == nil {
			key = values.Get("key")
		}
	}
	pages := make([]item, 0, len(files))
	for _, file := range files {
		if file == "" {
			return nil, "", errors.New("动漫屋返回了空图片地址")
		}
		if !strings.HasPrefix(file, "//") && !strings.HasPrefix(file, "https://") && !strings.HasPrefix(file, "http://") {
			file = prefix + file + suffix
		}
		imageURL, err := resolveURL(chapter, file)
		if err != nil || !dm5ImageHost(imageURL.Hostname()) {
			return nil, "", errors.New("动漫屋返回了未知的图片站点")
		}
		pages = append(pages, item{URL: imageURL.String()})
	}
	return pages, key, nil
}

func dm5ImageHost(host string) bool {
	return strings.HasSuffix(strings.ToLower(host), ".cdndm5.com")
}

type comicImageTransport struct{}

func (comicImageTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if strips := jmttImageStrips(request.URL); strips != 0 {
		request = request.Clone(request.Context())
		request.URL.Fragment = ""
		request.URL.RawFragment = ""
		response, err := http.DefaultTransport.RoundTrip(request)
		if err != nil || response.StatusCode != http.StatusOK {
			return response, err
		}
		return restoreJMTTResponse(response, strips)
	}
	if dm5ImageHost(request.URL.Hostname()) {
		request = request.Clone(request.Context())
		referer := dm5Origin
		if cid := request.URL.Query().Get("cid"); dm5Number.MatchString(cid) {
			referer += "m" + cid + "/"
		}
		request.Header.Set("Referer", referer)
	} else if request.URL.Scheme == "https" && request.URL.Hostname() == dumanwuImageHost {
		request = request.Clone(request.Context())
		request.Header.Set("Referer", dumanwuOrigin)
	}
	return http.DefaultTransport.RoundTrip(request)
}
