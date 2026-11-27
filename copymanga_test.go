package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestCopyMangaSearchUsesAlternateSiteOnConnectionFailure(t *testing.T) {
	var hosts []string
	client := &http.Client{Transport: cimocTestTransport(func(req *http.Request) (*http.Response, error) {
		hosts = append(hosts, req.URL.Host)
		if req.URL.Host == "www.copy4000.com" {
			return nil, io.ErrUnexpectedEOF
		}
		const body = `{"code":200,"results":{"list":[{"name":"電鋸人","path_word":"demo","cover":"/cover.jpg"}]}}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	hits, err := searchCopyManga(t.Context(), client, "电锯人")
	if err != nil || len(hits) != 1 || strings.Join(hosts, ",") != "www.copy4000.com,www.copy20.com" {
		t.Fatalf("search hits = %+v, attempted hosts = %v, error = %v", hits, hosts, err)
	}
	if hits[0].URL != copyMangaAlternateOrigin+"/comic/demo" || hits[0].Cover != copyMangaAlternateOrigin+"/cover.jpg" {
		t.Fatalf("result did not retain the working host: %+v", hits[0])
	}
}

func TestCopyMangaWebSource(t *testing.T) {
	const chapterID = "5f5d84c2-16c5-11f1-acc8-fa163e02432f"
	const key = "0123456789abcdef"
	encode := func(value string) string {
		block, err := aes.NewCipher([]byte(key))
		if err != nil {
			t.Fatal(err)
		}
		iv := []byte("abcdefghijklmnop")
		padding := aes.BlockSize - len(value)%aes.BlockSize
		data := append([]byte(value), bytes.Repeat([]byte{byte(padding)}, padding)...)
		cipher.NewCBCEncrypter(block, iv).CryptBlocks(data, data)
		return string(iv) + hex.EncodeToString(data)
	}
	chapterURL := copyMangaOrigin + "/comic/demo/chapter/" + chapterID
	client := &http.Client{Transport: cimocTestTransport(func(req *http.Request) (*http.Response, error) {
		var body string
		switch req.URL.Path {
		case "/api/kb/web/searchcl/comics":
			if req.URL.Query().Get("q") != "试读" || req.URL.Query().Get("platform") != "2" {
				return nil, fmt.Errorf("unexpected search query %s", req.URL.RawQuery)
			}
			body = `{"code":200,"results":{"list":[{"name":"试读","path_word":"demo","cover":"/cover.jpg"}]}}`
		case "/comic/demo":
			body = `<div class="comicParticulars-title-right"><h6>试读</h6></div><span id="dnt" value="9"></span><script>var ccz = '` + key + `';</script>`
		case "/comicdetail/demo/chapters":
			if req.Header.Get("dnts") != "9" || req.Referer() != copyMangaOrigin+"/comic/demo" {
				return nil, fmt.Errorf("missing chapter request headers")
			}
			body = fmt.Sprintf(`{"code":200,"results":%q}`, encode(`{"groups":{"default":{"chapters":[{"id":"`+chapterID+`","name":"第一话"}]}}}`))
		case "/comic/demo/chapter/" + chapterID:
			body = `<script>var cct = '` + key + `'; var contentKey = '` + encode(`[{"url":"https://sj.mangafunb.fun/p1.jpg"},{"url":"https://sj.mangafunb.fun/p2.jpg"}]`) + `';</script>`
		default:
			return nil, fmt.Errorf("unexpected request %s", req.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	result, ok := importCimoc([]byte(`{"COPYMH":"https://www.copy20.com"}`), true)
	if !ok || len(result.Sources) != 1 || result.Sources[0].readsInBrowser() {
		t.Fatalf("unexpected import result %+v", result)
	}
	src := result.Sources[0]
	hits, err := src.search(t.Context(), client, "试读")
	if err != nil || len(hits) != 1 || hits[0].Cover != copyMangaOrigin+"/cover.jpg" {
		t.Fatalf("search = %+v, err = %v", hits, err)
	}
	detail, chapters, err := src.loadComic(t.Context(), client, hits[0])
	if err != nil || detail.Title != "试读" || len(chapters) != 1 || chapters[0].URL != chapterURL {
		t.Fatalf("detail = %+v, chapters = %+v, err = %v", detail, chapters, err)
	}
	pages, err := fetchPages(t.Context(), src, client, chapters[0])
	if err != nil || len(pages) != 2 || pages[0].URL != "https://sj.mangafunb.fun/p1.jpg" || pages[1].URL != "https://sj.mangafunb.fun/p2.jpg" {
		t.Fatalf("pages = %+v, err = %v", pages, err)
	}
}
