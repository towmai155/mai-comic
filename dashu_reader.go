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

var (
	dashuReadPath       = regexp.MustCompile(`^/comic/[^/]+/read-[0-9]+\.html$`)
	dashuDesktopEnabled = regexp.MustCompile(`\bvar\s+stopswitch\s*=\s*1\s*;`)
	dashuImages         = regexp.MustCompile(`\bpicTree\s*=\s*(\[[^\]]*\])`)
)

func parseDashuPages(ctx context.Context, client *http.Client, chapter *url.URL) ([]item, error) {
	if chapter.Scheme != "https" || !dashuHost(chapter) || !dashuReadPath.MatchString(chapter.Path) {
		return nil, errors.New("大树漫画章节地址格式无效")
	}
	body, final, err := fetch(ctx, client, chapter, 8<<20)
	if err != nil {
		return nil, err
	}
	if !dashuHost(final) || !dashuReadPath.MatchString(final.Path) {
		return nil, errors.New("大树漫画没有提供公开阅读页")
	}
	source := string(body)
	switchAt := dashuDesktopEnabled.FindStringIndex(source)
	if switchAt == nil {
		return nil, errors.New("大树漫画未开放本章的桌面读图")
	}
	decoded, err := unpackReaderScript(source[switchAt[1]:])
	if err != nil {
		return nil, err
	}
	var addresses []string
	if images := dashuImages.FindStringSubmatch(decoded); len(images) == 0 ||
		json.Unmarshal([]byte(images[1]), &addresses) != nil || len(addresses) == 0 {
		return nil, errors.New("大树漫画没有返回可识别的图片列表")
	}
	pages := make([]item, 0, len(addresses))
	for _, address := range addresses {
		if strings.TrimSpace(address) == "" {
			return nil, errors.New("大树漫画返回了空图片地址")
		}
		image, err := resolveURL(final, address)
		if err != nil {
			return nil, err
		}
		pages = append(pages, item{URL: image.String()})
	}
	return pages, nil
}

func dashuHost(target *url.URL) bool {
	return target.Hostname() == "www.dashumanhua.com" || target.Hostname() == "dashumanhua.com"
}
