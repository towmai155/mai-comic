package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestTencentReaderHonorsPublishedAccess(t *testing.T) {
	chapter := &url.URL{Scheme: "https", Host: "ac.qq.com", Path: "/ComicView/index/id/1/cid/2"}
	const address = "https://manhua.acimg.cn/reading/first.jpg"
	for _, canRead := range []bool{true, false} {
		data := fmt.Sprintf(`{"chapter":{"canRead":%t},"picture":[{"url":%q}]}`, canRead, address)
		encoded := base64.StdEncoding.EncodeToString([]byte(data))
		packed := encoded[:21] + "qx" + encoded[21:]
		html := `<script>window["no"+"nce"]='unused';window["n"+"once"]="2"+(+eval("Math.pow(1,3)")).toString()+"qx";var DATA='` + packed + `';</script>`
		pages, err := decodeTencentPages(html, chapter)
		if !canRead && (err == nil || len(pages) != 0) {
			t.Fatal("closed chapter returned image URLs")
		}
		if canRead && (err != nil || len(pages) != 1 || pages[0].URL != address) {
			t.Fatalf("pages = %+v, error = %v", pages, err)
		}
	}
}

func TestTencentNonceParsesBrowserExpressions(t *testing.T) {
	input := `"a" + (+eval("!!document.getElementsByTagName('html')")).toString() +
		(+eval("1 - !!document.children")).toString() +
		(+eval("1 * (!window.Array) + 1")).toString() +
		(+eval("5&5")).toString() +
		(+eval("0<=3?2:1")).toString() +
		(+eval("'123'.substring(2)")).toString()`
	if got, ok := parseTencentNonce(input); !ok || got != "a101523" {
		t.Fatalf("nonce = %q, valid = %t", got, ok)
	}
}

func TestDumanwuReaderParsesPublishedManifest(t *testing.T) {
	chapter := &url.URL{Scheme: "https", Host: "www.dumanwu.org", Path: "/comic/fixture.html"}
	const address = "https://dmw.546457.xyz/reading/first.webp"
	data := []byte(`{"host":"www.dumanwu.org","images":["` + address + `"]}`)
	n := aes.BlockSize - len(data)%aes.BlockSize
	data = append(data, bytes.Repeat([]byte{byte(n)}, n)...)
	key, err := aes.NewCipher([]byte(dumanwuReaderKey))
	if err != nil {
		t.Fatal(err)
	}
	iv := bytes.Repeat([]byte{7}, aes.BlockSize)
	cipher.NewCBCEncrypter(key, iv).CryptBlocks(data, data)
	html := []byte("<script>var params = '" + base64.StdEncoding.EncodeToString(append(iv, data...)) + "';</script>")
	pages, err := decodeDumanwuPages(html, chapter)
	if err != nil || len(pages) != 1 || pages[0].URL != address {
		t.Fatalf("pages = %+v, error = %v", pages, err)
	}
	chapter.Host = "example.test"
	if _, err := decodeDumanwuPages(html, chapter); err == nil {
		t.Fatal("accepted a manifest for a different host")
	}
}

func TestDashuReaderRespectsDesktopAvailability(t *testing.T) {
	src := source{ID: "cimoc-DASHUMANHUA", BaseURL: "https://www.dashumanhua.com", BrowserReader: true}
	if src.readsInBrowser() {
		t.Fatal("previously imported source should use the built-in reader")
	}
	const address = "https://res.taozi.icu/reading/first.jpg"
	const packed = `eval(function(){ }('0 1=["` + address + `"];',36,2,'var|picTree'.split('|'),0,{}))`
	allowed := 1
	client := &http.Client{Transport: cimocTestTransport(func(r *http.Request) (*http.Response, error) {
		body := fmt.Sprintf("<script>var stopswitch = %d; %s</script>", allowed, packed)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	chapter := item{URL: src.BaseURL + "/comic/fixture/read-0.html"}
	pages, err := fetchPages(t.Context(), src, client, chapter)
	if err != nil || len(pages) != 1 || pages[0].URL != address {
		t.Fatalf("pages = %+v, error = %v", pages, err)
	}
	allowed = 0
	if pages, err := fetchPages(t.Context(), src, client, chapter); err == nil || len(pages) != 0 {
		t.Fatal("disabled desktop reader returned image URLs")
	}
}

func TestKanmanReaderUsesOnlyTheCurrentPublicChapter(t *testing.T) {
	src, ok := adaptCimocSource("MANHUATAI", "https://m.kanman.com")
	if !ok || src.readsInBrowser() {
		t.Fatal("漫画台 must use the built-in reader")
	}
	chapter := item{URL: src.BaseURL + "/1/di1hua-2.html"}
	const first = "https://images.kaimanhua.com/current/1.jpg?auth_key=issued"
	const next = "https://images.kaimanhua.com/next/1.jpg"
	for _, price := range []int{0, 49} {
		client := &http.Client{Transport: cimocTestTransport(func(r *http.Request) (*http.Response, error) {
			body := fmt.Sprintf(`<script>window.comicInfo={current_chapter:{chapter_newid:"di1hua-2",rule:"page/{i}.jpg",price:%d,is_vip:0,chapter_img_list:[%q]},next_chapter:{chapter_img_list:[%q]}};</script>`, price, first, next)
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		})}
		pages, err := fetchPages(t.Context(), src, client, chapter)
		if price > 0 {
			if err == nil || len(pages) != 0 {
				t.Fatal("paid chapter returned images")
			}
		} else if err != nil || len(pages) != 1 || pages[0].URL != first {
			t.Fatalf("pages = %+v, error = %v", pages, err)
		}
	}
}

func TestMKZhanReaderChecksAccessBeforeRequestingImages(t *testing.T) {
	src, ok := adaptCimocSource("MKZHAN", "https://comic.mkzhan.com")
	if !ok || src.readsInBrowser() {
		t.Fatal("漫客栈 must use the built-in reader")
	}
	chapter := item{URL: src.BaseURL + "/1/2.html"}
	for _, price := range []int{0, 49} {
		client := &http.Client{Transport: cimocTestTransport(func(r *http.Request) (*http.Response, error) {
			q := r.URL.Query()
			if r.URL.Host != "comic.mkzcdn.com" || q.Get("comic_id") != "1" || q.Get("chapter_id") != "2" {
				t.Fatalf("incorrect public chapter request: %s", r.URL)
			}
			var body string
			switch r.URL.Path {
			case "/chapter/info/":
				body = fmt.Sprintf(`{"code":"200","data":{"status":"1","is_vip":0,"price":%d}}`, price)
			case "/chapter/content/v1/":
				if price > 0 {
					t.Fatal("requested images for a paid chapter")
				}
				body = `{"code":"200","data":{"page":[{"image":"http://content.mkzcdn.com/1.jpg"}]}}`
			default:
				t.Fatalf("unexpected request: %s", r.URL)
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		})}
		pages, err := fetchPages(t.Context(), src, client, chapter)
		if price > 0 {
			if err == nil || len(pages) != 0 {
				t.Fatal("paid chapter returned images")
			}
		} else if err != nil || len(pages) != 1 || pages[0].URL != "https://content.mkzcdn.com/1.jpg" {
			t.Fatalf("pages = %+v, error = %v", pages, err)
		}
	}
}
