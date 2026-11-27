package main

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
)

var manga1234Redirect = regexp.MustCompile(`location\.replace\(\s*"(https://reader\.hqread\.cc/r/[A-Za-z0-9_=-]+)"\s*\)`)

var manga1234PageRule = rule{
	Format: "html", Items: "img.reader-image[data-pic-id][data-src]",
	Fields: map[string]string{"url": "@data-src"},
}

func parseManga1234Pages(ctx context.Context, client *http.Client, chapter *url.URL) ([]item, error) {
	if !manga1234ChapterHost(chapter.Hostname()) {
		return nil, errors.New("漫画1234 章节地址不属于已知阅读站点")
	}
	body, final, err := fetch(ctx, client, chapter, 8<<20)
	if err != nil {
		return nil, err
	}
	if !manga1234ChapterHost(final.Hostname()) {
		return nil, errors.New("漫画1234 章节跳转到了未知阅读站点")
	}
	// The site's public chapter link can land on a page with a script redirect.
	if redirect := manga1234Redirect.FindSubmatch(body); len(redirect) > 0 && final.Hostname() != "reader.hqread.cc" {
		reader, err := resolveURL(final, string(redirect[1]))
		if err != nil {
			return nil, err
		}
		body, final, err = fetch(ctx, client, reader, 8<<20)
		if err != nil {
			return nil, err
		}
		if final.Hostname() != "reader.hqread.cc" {
			return nil, errors.New("漫画1234 阅读页跳转到了未知站点")
		}
	}
	pages, _, err := manga1234PageRule.parsePage(body, final, false)
	return pages, err
}

func manga1234ChapterHost(host string) bool {
	return host == "www.wmh1234.com" || host == "wmh1234.com" || host == "reader.hqread.cc"
}
