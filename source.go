package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"
)

type sourceFile struct {
	Version int      `json:"version"`
	Sources []source `json:"sources"`
}

type sourceImport struct {
	Sources []source
	Skipped []sourceSkip
	Cimoc   bool
}

type sourceSkip struct {
	ID      string `json:"id"`
	Reason  string `json:"reason"`
	Website string `json:"website,omitempty"`
}

type source struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	BaseURL       string `json:"baseUrl"`
	Search        rule   `json:"search"`
	Details       rule   `json:"details"`
	Chapters      rule   `json:"chapters"`
	Pages         rule   `json:"pages"`
	BrowserReader bool   `json:"browserReader,omitempty"`
}

type rule struct {
	Format string            `json:"format"`
	URL    string            `json:"url,omitempty"`
	Items  string            `json:"items,omitempty"`
	Next   string            `json:"next,omitempty"`
	Fields map[string]string `json:"fields"`

	searchResult bool
}

type item struct {
	Title   string
	URL     string
	Summary string
	Cover   string `json:",omitempty"`
}

var sourceID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

func sourceConfigURL(address string) string {
	address = strings.TrimSpace(address)
	if address == cimocLegacyURL {
		return cimocRawURL
	}
	return address
}

func importSources(ctx context.Context, client *http.Client, address string) (sourceImport, error) {
	address = sourceConfigURL(address)
	target, err := resolveURL(nil, address)
	if err != nil {
		return sourceImport{}, err
	}
	body, responseURL, err := fetch(ctx, client, target, 1<<20)
	if err != nil {
		return sourceImport{}, err
	}
	if src, ok := importJMTTWebsite(target, responseURL); ok {
		if err := src.validate(); err != nil {
			return sourceImport{}, err
		}
		return sourceImport{Sources: []source{src}}, nil
	}
	if isZaimanhuaHome(target) && isZaimanhuaHome(responseURL) {
		src, _ := adaptCimocSource("HAOMAN8", responseURL.String())
		if err := src.validate(); err != nil {
			return sourceImport{}, err
		}
		return sourceImport{Sources: []source{src}}, nil
	}
	var file sourceFile
	if err := json.Unmarshal(body, &file); err != nil {
		return sourceImport{}, fmt.Errorf("图源地址没有返回有效的 JSON 配置：%w", err)
	}
	if file.Version != 1 || len(file.Sources) == 0 {
		if file.Version == 0 && file.Sources == nil {
			if result, ok := importCimoc(body, address == cimocRawURL); ok {
				if len(result.Sources) == 0 && len(result.Skipped) == 0 {
					return sourceImport{}, errors.New("这份 Cimoc 配置没有匹配本地适配器或带有完整规则的图源")
				}
				return result, nil
			}
		}
		return sourceImport{}, errors.New("需要 version 为 1 且包含至少一个图源的 JSON 配置")
	}
	seen := make(map[string]bool)
	for _, src := range file.Sources {
		if seen[src.ID] {
			return sourceImport{}, fmt.Errorf("图源 ID 重复：%s", src.ID)
		}
		if err := src.validate(); err != nil {
			return sourceImport{}, fmt.Errorf("图源 %q：%w", src.Name, err)
		}
		seen[src.ID] = true
	}
	return sourceImport{Sources: file.Sources}, nil
}

type sourceUpdate struct {
	address string
	result  sourceImport
	err     error
}

func fetchSourceUpdates(ctx context.Context, client *http.Client, addresses []string) []sourceUpdate {
	updates := make([]sourceUpdate, 0, len(addresses))
	for _, address := range addresses {
		if ctx.Err() != nil {
			break
		}
		result, err := importSources(ctx, client, address)
		updates = append(updates, sourceUpdate{address: address, result: result, err: err})
	}
	return updates
}

func (s source) validate() error {
	if !sourceID.MatchString(s.ID) || strings.TrimSpace(s.Name) == "" {
		return errors.New("需要有效的 id 和 name")
	}
	base, err := resolveURL(nil, s.BaseURL)
	if err != nil {
		return fmt.Errorf("baseUrl：%w", err)
	}
	adapter := s.cimocAdapter()
	usesSearchRules := s.Search.Format != "" || adapter.search == nil
	usesComicRules := s.Details.Format != "" || s.Chapters.Format != "" || adapter.loadComic == nil
	if usesSearchRules {
		if !strings.Contains(s.Search.URL, "{query}") {
			return errors.New("search.url 必须包含 {query}")
		}
		if _, err := resolveURL(base, strings.ReplaceAll(s.Search.URL, "{query}", "test")); err != nil {
			return fmt.Errorf("search.url：%w", err)
		}
	}
	for _, entry := range []struct {
		name string
		rule rule
		keys []string
	}{
		{"search", s.Search, []string{"title", "url"}},
		{"details", s.Details, []string{"title"}},
		{"chapters", s.Chapters, []string{"title", "url"}},
		{"pages", s.Pages, []string{"url"}},
	} {
		if entry.name == "search" && !usesSearchRules {
			continue
		}
		if (entry.name == "details" || entry.name == "chapters") && !usesComicRules {
			continue
		}
		if entry.name == "pages" && (s.BrowserReader || s.chapterParser() != nil) {
			continue
		}
		if entry.rule.Next != "" && entry.name != "chapters" {
			return fmt.Errorf("%s：next 只用于章节列表", entry.name)
		}
		if err := entry.rule.validate(entry.keys); err != nil {
			return fmt.Errorf("%s：%w", entry.name, err)
		}
	}
	return nil
}

func (r rule) validate(required []string) error {
	if r.Format != "json" && r.Format != "html" {
		return errors.New("format 必须是 html 或 json")
	}
	if r.Next != "" {
		if r.Format != "html" {
			return errors.New("next 需要 html 格式")
		}
		selector, attr := htmlField(r.Next)
		if selector == "" || attr != "href" {
			return errors.New("next 需要下一页链接的 CSS 选择器和 @href")
		}
		if _, err := cascadia.Compile(selector); err != nil {
			return fmt.Errorf("next 选择器：%w", err)
		}
	}
	for _, name := range required {
		if strings.TrimSpace(r.Fields[name]) == "" {
			return fmt.Errorf("缺少 fields.%s", name)
		}
	}
	if r.Format == "html" {
		if r.Items != "" {
			if _, err := cascadia.Compile(r.Items); err != nil {
				return fmt.Errorf("items 选择器：%w", err)
			}
		}
		for name, field := range r.Fields {
			selector, _ := htmlField(field)
			if selector != "" && selector != "." {
				if _, err := cascadia.Compile(selector); err != nil {
					return fmt.Errorf("fields.%s：%w", name, err)
				}
			}
		}
	}
	return nil
}

func (s source) search(ctx context.Context, client *http.Client, query string) ([]item, error) {
	adapter := s.cimocAdapter()
	search := s.Search
	if search.Format == "" && adapter.search != nil {
		return adapter.search(ctx, client, query)
	}
	// Stored Cimoc rules used to select the title link without the adjacent cover.
	if adapter.previousSearchItems != "" && search.Items == adapter.previousSearchItems &&
		search.Format == "html" && search.URL == adapter.rules.Search.URL &&
		len(search.Fields) == 2 && search.Fields["title"] == "." && search.Fields["url"] == "@href" {
		search = adapter.rules.Search
	}
	search.searchResult = true
	return search.run(ctx, client, s.BaseURL, strings.ReplaceAll(search.URL, "{query}", url.QueryEscape(query)), false)
}

func (s source) loadComic(ctx context.Context, client *http.Client, comic item) (item, []item, error) {
	if load := s.cimocAdapter().loadComic; s.Details.Format == "" && s.Chapters.Format == "" && load != nil {
		return load(ctx, client, comic)
	}
	detail, err := s.comicDetails(ctx, client, comic)
	if err != nil {
		return item{}, nil, err
	}
	chapters, err := s.Chapters.run(ctx, client, comic.URL, s.Chapters.URL, false)
	return detail, chapters, err
}

func (s source) comicDetails(ctx context.Context, client *http.Client, comic item) (item, error) {
	if load := s.cimocAdapter().loadComic; s.Details.Format == "" && load != nil {
		detail, _, err := load(ctx, client, comic)
		return detail, err
	}
	details := s.Details
	if details.Format == "html" && details.Fields["cover"] == "" {
		if field := s.cimocAdapter().rules.Details.Fields["cover"]; field != "" {
			details.Fields = maps.Clone(details.Fields)
			details.Fields["cover"] = field
		}
	}
	items, err := details.run(ctx, client, comic.URL, details.URL, true)
	if err != nil {
		return item{}, err
	}
	return items[0], nil
}

func (r rule) run(ctx context.Context, client *http.Client, current, path string, single bool) ([]item, error) {
	base, err := resolveURL(nil, current)
	if err != nil {
		return nil, err
	}
	target, err := resolveURL(base, path)
	if err != nil {
		return nil, err
	}
	items, next, err := r.runPage(ctx, client, target, single)
	if err != nil || next == nil {
		return items, err
	}
	visited := map[string]bool{target.String(): true}
	for next != nil {
		if visited[next.String()] {
			return nil, errors.New("章节列表出现重复分页")
		}
		if len(visited) >= 200 {
			return nil, errors.New("章节列表超过 200 页")
		}
		visited[next.String()] = true
		var page []item
		page, next, err = r.runPage(ctx, client, next, single)
		if err != nil {
			return nil, err
		}
		items = append(items, page...)
	}
	return items, nil
}

func (r rule) runPage(ctx context.Context, client *http.Client, target *url.URL, single bool) ([]item, *url.URL, error) {
	body, responseURL, err := fetch(ctx, client, target, 8<<20)
	if err != nil {
		return nil, nil, err
	}
	return r.parsePage(body, responseURL, single)
}

func (r rule) parsePage(body []byte, responseURL *url.URL, single bool) ([]item, *url.URL, error) {
	var rows []func(string) string
	var nextPath, defaultCover string
	var err error
	if r.Format == "html" {
		rows, nextPath, defaultCover, err = r.htmlRows(body, responseURL, single)
	} else {
		rows, err = r.jsonRows(body, single)
	}
	if err != nil {
		return nil, nil, err
	}
	items := make([]item, 0, len(rows))
	for i, field := range rows {
		result := item{Title: strings.TrimSpace(field(r.Fields["title"])), Summary: strings.TrimSpace(field(r.Fields["summary"]))}
		result.Cover = coverURL(responseURL, field(r.Fields["cover"]))
		if result.Cover == "" && r.searchResult {
			fields := []string{"cover", "coverUrl", "cover_url", "thumbnail"}
			if r.Format == "html" {
				fields = []string{
					"img[data-original]@data-original", "img[data-src], amp-img[data-src], mip-img[data-src]@data-src",
					"img[data-echo]@data-echo", "img[data-url]@data-url", "img[src], amp-img[src], mip-img[src]@src",
				}
			}
			for _, spec := range fields {
				if result.Cover = coverURL(responseURL, field(spec)); result.Cover != "" {
					break
				}
			}
		}
		if result.Cover == "" {
			result.Cover = defaultCover
		}
		if fallback := r.Fields["fallbackTitle"]; result.Title == "" && fallback != "" {
			result.Title = strings.TrimSpace(field(fallback))
		}
		if r.Fields["title"] != "" && result.Title == "" {
			return nil, nil, fmt.Errorf("第 %d 项缺少标题，请检查图源规则", i+1)
		}
		if r.Fields["url"] != "" {
			raw := strings.TrimSpace(field(r.Fields["url"]))
			if raw == "" {
				return nil, nil, fmt.Errorf("第 %d 项缺少链接，请检查图源规则", i+1)
			}
			link, err := resolveURL(responseURL, raw)
			if err != nil {
				return nil, nil, fmt.Errorf("第 %d 项的链接：%w", i+1, err)
			}
			result.URL = link.String()
		}
		items = append(items, result)
	}
	if nextPath = strings.TrimSpace(nextPath); nextPath != "" {
		next, err := resolveURL(responseURL, nextPath)
		if err != nil {
			return nil, nil, fmt.Errorf("章节列表下一页：%w", err)
		}
		next.Fragment = ""
		if next.Scheme != responseURL.Scheme || !strings.EqualFold(next.Host, responseURL.Host) {
			return nil, nil, errors.New("章节列表的分页链接需要与当前页面同源")
		}
		return items, next, nil
	}
	return items, nil, nil
}

func (r rule) jsonRows(body []byte, single bool) ([]func(string) string, error) {
	var doc any
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("响应不是有效的 JSON：%w", err)
	}
	value := jsonPath(doc, r.Items)
	items, ok := value.([]any)
	if single {
		items = []any{value}
	} else if !ok {
		return nil, fmt.Errorf("JSON 列表路径 %q 没有匹配到数组", r.Items)
	}
	rows := make([]func(string) string, 0, len(items))
	for _, item := range items {
		rows = append(rows, func(path string) string {
			if path == "" {
				return ""
			}
			value, _ := jsonPath(item, path).(string)
			return value
		})
	}
	return rows, nil
}

func jsonPath(value any, path string) any {
	if path == "" || path == "." {
		return value
	}
	for part := range strings.SplitSeq(path, ".") {
		object, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		value = object[part]
	}
	return value
}

func (r rule) htmlRows(body []byte, base *url.URL, single bool) ([]func(string) string, string, string, error) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, "", "", err
	}
	nodes := []*html.Node{doc}
	if r.Items != "" {
		selector, err := cascadia.Compile(r.Items)
		if err != nil {
			return nil, "", "", err
		}
		nodes = cascadia.QueryAll(doc, selector)
	}
	if single && len(nodes) != 1 {
		return nil, "", "", errors.New("详情规则需要匹配到一项")
	}
	rows := make([]func(string) string, 0, len(nodes))
	for _, node := range nodes {
		rows = append(rows, func(spec string) string {
			return htmlValue(node, spec)
		})
	}
	var cover string
	if single {
		for _, field := range []string{
			`meta[property="og:image:secure_url"]@content`,
			`meta[property="og:image"]@content`,
			`meta[name="twitter:image"]@content`,
			`img[itemprop="image"]@src`,
		} {
			if cover = coverURL(base, htmlValue(doc, field)); cover != "" {
				break
			}
		}
	}
	return rows, htmlValue(doc, r.Next), cover, nil
}

func coverURL(base *url.URL, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw != "" {
		if address, err := resolveURL(base, raw); err == nil {
			return address.String()
		}
	}
	return ""
}

func htmlValue(node *html.Node, spec string) string {
	if spec == "" {
		return ""
	}
	selector, attr := htmlField(spec)
	if selector != "" && selector != "." {
		compiled, err := cascadia.Compile(selector)
		if err != nil {
			return ""
		}
		node = cascadia.Query(node, compiled)
	}
	if node == nil {
		return ""
	}
	if attr != "" {
		for _, attribute := range node.Attr {
			if attribute.Key == attr {
				return attribute.Val
			}
		}
		return ""
	}
	var text strings.Builder
	for descendant := range node.Descendants() {
		if descendant.Type == html.TextNode {
			text.WriteString(descendant.Data)
		}
	}
	return text.String()
}

func htmlField(spec string) (selector, attr string) {
	if index := strings.LastIndex(spec, "@"); index >= 0 && !strings.ContainsAny(spec[index+1:], " []\t'\"") {
		return spec[:index], spec[index+1:]
	}
	return spec, ""
}

func resolveURL(base *url.URL, value string) (*url.URL, error) {
	ref, err := url.Parse(value)
	if err != nil {
		return nil, errors.New("链接格式无效")
	}
	if base != nil {
		ref = base.ResolveReference(ref)
	}
	if (ref.Scheme != "http" && ref.Scheme != "https") || ref.Hostname() == "" || ref.User != nil {
		return nil, errors.New("需要有效的 HTTP 或 HTTPS 链接")
	}
	return ref, nil
}

func fetch(ctx context.Context, client *http.Client, address *url.URL, limit int64) ([]byte, *url.URL, error) {
	return fetchWithReferer(ctx, client, address, limit, "")
}

func fetchWithReferer(ctx context.Context, client *http.Client, address *url.URL, limit int64, referer string) ([]byte, *url.URL, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address.String(), nil)
	if err != nil {
		return nil, nil, err
	}
	if referer != "" {
		request.Header.Set("Referer", referer)
	}
	return fetchRequest(client, request, limit)
}

func fetchRequest(client *http.Client, request *http.Request, limit int64) ([]byte, *url.URL, error) {
	response, err := client.Do(request)
	if err != nil {
		return nil, nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, nil, fmt.Errorf("请求返回 HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, nil, err
	}
	if int64(len(data)) > limit {
		return nil, nil, errors.New("响应数据超过大小限制")
	}
	return data, response.Request.URL, nil
}
