// Package demosource serves original, local content for trying the reader.
package demosource

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"net/http"
	"strconv"
	"strings"
)

func Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /sources.json", func(w http.ResponseWriter, r *http.Request) {
		body := strings.ReplaceAll(config, "BASE", "http://"+r.Host)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, body)
	})
	mux.HandleFunc("GET /json/search", func(w http.ResponseWriter, r *http.Request) {
		items := []map[string]string{}
		if matches(r, "纸上的远足") {
			items = append(items, map[string]string{"name": "纸上的远足", "href": "/json/comic", "cover": "/page/1.png"})
		}
		replyJSON(w, map[string]any{"results": items})
	})
	mux.HandleFunc("GET /json/comic", func(w http.ResponseWriter, _ *http.Request) {
		replyJSON(w, map[string]any{
			"name": "纸上的远足", "intro": "用七张抽象分镜试读，体验搜索、翻章和进度恢复。",
			"cover": "/page/1.png",
			"chapters": []map[string]string{
				{"name": "第一章 · 出发", "href": "/json/chapter/1"},
				{"name": "第二章 · 归途", "href": "/json/chapter/2"},
			},
		})
	})
	mux.HandleFunc("GET /json/chapter/{id}", func(w http.ResponseWriter, r *http.Request) {
		start, end := 1, 4
		if r.PathValue("id") == "2" {
			start, end = 5, 7
		}
		pages := make([]string, 0, end-start+1)
		for n := start; n <= end; n++ {
			pages = append(pages, fmt.Sprintf("/page/%d.png", n))
		}
		replyJSON(w, map[string]any{"images": pages})
	})
	mux.HandleFunc("GET /html/search", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if matches(r, "纸灯") {
			io.WriteString(w, `<a class="comic" href="/html/comic"><img src="data:image/gif;base64,R0lGODlhAQABAAAAACw=" data-original="../page/8.png">纸灯</a>`)
		}
	})
	mux.HandleFunc("GET /html/comic", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, `<meta property="og:image" content="/page/8.png">
			<h1>纸灯</h1><p class="intro">内容来自 HTML 图源，中间共用同一套阅读器。</p>
			<a class="chapter" href="/html/chapter">第一章 · 点亮</a>`)
	})
	mux.HandleFunc("GET /html/chapter", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		for n := 8; n <= 10; n++ {
			fmt.Fprintf(w, `<img class="page" src="/page/%d.png">`, n)
		}
	})
	mux.HandleFunc("GET /page/{file}", servePage)
	return mux
}

func matches(r *http.Request, title string) bool {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	return q != "" && strings.Contains(title, q)
}

func replyJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(value)
}

func servePage(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(strings.TrimSuffix(r.PathValue("file"), ".png"))
	if err != nil || n < 1 || n > 10 {
		http.NotFound(w, r)
		return
	}
	canvas := image.NewRGBA(image.Rect(0, 500, 720, 1500))
	fill := func(bounds image.Rectangle, ink color.RGBA) {
		draw.Draw(canvas, bounds, image.NewUniform(ink), image.Point{}, draw.Src)
	}
	shades := []color.RGBA{{40, 62, 75, 255}, {76, 55, 91, 255}, {97, 59, 51, 255}, {46, 82, 68, 255}}
	sky := shades[(n-1)%len(shades)]
	paper := color.RGBA{246, 238, 219, 255}
	light := color.RGBA{243, 183, 94, 255}
	fill(canvas.Bounds(), paper)
	for panel := range 3 {
		y := 555 + panel*296
		fill(image.Rect(45, y, 675, y+263), sky)
		for line := range 9 {
			h := y + 115 + (n*17+line*line*3+panel*29)%110
			x := 65 + line*65
			fill(image.Rect(x, h, x+56, y+245), color.RGBA{sky.R + 13, sky.G + 13, sky.B + 13, 255})
		}
		x := 108 + (n*71+panel*137)%452
		fill(image.Rect(x, y+35, x+41, y+76), light)
		fill(image.Rect(x+14, y+72, x+28, y+110), light)
	}
	for tick := range n {
		x := 46 + tick*18
		fill(image.Rect(x, 1447, x+11, 1458), sky)
	}
	w.Header().Set("Content-Type", "image/png")
	png.Encode(w, canvas)
}

const config = `{
  "version": 1,
  "sources": [
    {
      "id": "demo-json",
      "name": "试读 · JSON 图源",
      "baseUrl": "BASE",
      "search": {"format":"json","url":"/json/search?q={query}","items":"results","fields":{"title":"name","url":"href","cover":"cover"}},
      "details": {"format":"json","fields":{"title":"name","summary":"intro","cover":"cover"}},
      "chapters": {"format":"json","items":"chapters","fields":{"title":"name","url":"href"}},
      "pages": {"format":"json","items":"images","fields":{"url":"."}}
    },
    {
      "id": "demo-html",
      "name": "试读 · HTML 图源",
      "baseUrl": "BASE",
      "search": {"format":"html","url":"/html/search?q={query}","items":"a.comic","fields":{"title":".","url":"@href"}},
      "details": {"format":"html","fields":{"title":"h1","summary":".intro"}},
      "chapters": {"format":"html","items":"a.chapter","fields":{"title":".","url":"@href"}},
      "pages": {"format":"html","items":"img.page","fields":{"url":"@src"}}
    }
  ]
}`
