package main

import (
	"context"
	"io"
	"log"
	"net/http"
	"runtime"

	"github.com/egoist/mygo/ui"
	"mygo-manga/internal/comicreader"
)

type nativeReading struct {
	view       comicreader.Reader
	session    io.Closer
	generation uint64
	dark       bool
}

type readerImageLoader struct{ client *http.Client }

func (l readerImageLoader) Load(ctx context.Context, address string) ([]byte, error) {
	target, err := resolveURL(nil, address)
	if err != nil {
		return nil, err
	}
	body, _, err := fetch(ctx, l.client, target, 64<<20)
	return body, err
}

func (a *app) openNativeChapter() {
	if runtime.GOOS != "android" || a.win == nil {
		return
	}
	a.closeNativeChapter()
	native := &a.nativeReader
	if native.view == nil {
		view, err := comicreader.NewReader()
		if err != nil {
			log.Printf("android comic reader: %v", err)
			return
		}
		native.view = view
	}
	pages := make([]string, len(a.pages))
	for i, page := range a.pages {
		pages[i] = page.URL
	}
	chapter, generation := a.chapter, native.generation
	next := a.chapterNeighbor(chapter, 1)
	isCurrent := func() bool { return native.generation == generation && a.chapter == chapter }
	loader := readerImageLoader{client: &http.Client{Timeout: a.client.Timeout, Transport: comicImageTransport{}}}
	native.session = native.view.Open(pages, a.firstPage, next >= 0, native.dark, loader,
		func(page int) {
			if isCurrent() && page >= 0 && page < len(a.pages) && a.firstPage != page {
				a.firstPage = page
				if a.readerControls {
					a.win.Invalidate()
				}
			}
		},
		func() {
			if isCurrent() {
				a.toggleReaderControls()
				a.win.Invalidate()
			}
		},
		func() {
			if isCurrent() && next >= 0 && a.cancel == nil {
				a.firstPage = len(a.pages) - 1
				a.openChapter(next)
			}
		},
	)
}

func (a *app) closeNativeChapter() {
	native := &a.nativeReader
	native.generation++
	if native.session != nil {
		closer := native.session
		native.session = nil
		if err := closer.Close(); err != nil {
			log.Printf("close android comic chapter: %v", err)
		}
	}
}

func (a *app) presentNativeReader(c *ui.Context) bool {
	native := &a.nativeReader
	if native.session == nil {
		return false
	}
	if dark := c.Theme().Dark; dark != native.dark {
		native.dark = dark
		native.view.SetDark(dark)
	}
	ui.NativeView(c, native.view).Grow(1).Basis(0)
	return true
}

func (a *app) seekReaderPage(page int) {
	a.firstPage = page
	if a.nativeReader.session != nil {
		a.nativeReader.view.Seek(page)
	} else {
		a.reader.ScrollTo(page, ui.Start)
	}
}
