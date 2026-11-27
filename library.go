package main

import (
	"context"
	"fmt"
	"slices"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/netimage"
)

type favorite struct {
	SourceID string `json:"sourceId"`
	Comic    item   `json:"comic"`
}

type progress struct {
	SourceID string `json:"sourceId"`
	Comic    item   `json:"comic"`
	Chapter  item   `json:"chapter"`
	Page     int    `json:"page"` // -1 for a chapter opened in the browser.
}

func (p progress) description() string {
	if p.Page < 0 {
		return p.Chapter.Title + " · 原站阅读"
	}
	return fmt.Sprintf("%s · 第 %d 页", p.Chapter.Title, p.Page+1)
}

type library struct {
	ChaptersDescending bool             `json:"chaptersDescending,omitempty"`
	StrictSearch       bool             `json:"strictSearch,omitempty"`
	Appearance         mygo.ThemeSource `json:"appearance,omitempty"`
	Sources            []source         `json:"sources"`
	SourceURLs         []string         `json:"sourceUrls,omitempty"`
	Favorites          []favorite       `json:"favorites,omitempty"`
	History            []progress       `json:"history,omitempty"`
	Last               *progress        `json:"last,omitempty"`
	CimocReport        []sourceSkip     `json:"cimocReport,omitempty"`
}

func (l *library) applySourceImport(address string, result sourceImport) {
	if !slices.Contains(l.SourceURLs, address) {
		l.SourceURLs = append(l.SourceURLs, address)
	}
	for _, src := range result.Sources {
		i := slices.IndexFunc(l.Sources, func(old source) bool { return old.ID == src.ID })
		if i >= 0 {
			l.Sources[i] = src
		} else {
			l.Sources = append(l.Sources, src)
		}
	}
	if result.Cimoc {
		l.CimocReport = result.Skipped
	}
}

func (l *library) favoriteIndex(id, url string) int {
	return slices.IndexFunc(l.Favorites, func(entry favorite) bool {
		return entry.SourceID == id && entry.Comic.URL == url
	})
}

func (l *library) historyIndex(id, url string) int {
	return slices.IndexFunc(l.History, func(entry progress) bool {
		return entry.SourceID == id && entry.Comic.URL == url
	})
}

func (l *library) updateLast() {
	l.Last = nil
	if len(l.History) > 0 {
		latest := l.History[0]
		l.Last = &latest
	}
}

func (a *app) toggleFavorite() {
	id, comic := a.comicSource, a.comic
	if i := a.library.favoriteIndex(id, comic.URL); i >= 0 {
		a.removeFavorite(id, comic.URL)
		return
	}
	if a.detail.Title != "" {
		comic.Title = a.detail.Title
	}
	a.library.Favorites = slices.Insert(a.library.Favorites, 0, favorite{SourceID: id, Comic: comic})
	a.save()
}

func (a *app) removeFavorite(id, url string) {
	if i := a.library.favoriteIndex(id, url); i >= 0 {
		a.library.Favorites = slices.Delete(a.library.Favorites, i, i+1)
		a.save()
	}
}

func (a *app) recordProgress(p progress) {
	i := a.library.historyIndex(p.SourceID, p.Comic.URL)
	if i >= 0 && p.Comic.Cover == "" {
		p.Comic.Cover = a.library.History[i].Comic.Cover
	}
	if i == 0 && a.library.History[0] == p {
		return
	}
	if i >= 0 {
		a.library.History = slices.Delete(a.library.History, i, i+1)
	}
	a.library.History = slices.Insert(a.library.History, 0, p)
	a.library.updateLast()
	a.save()
}

func (a *app) removeHistory(p progress) {
	if i := a.library.historyIndex(p.SourceID, p.Comic.URL); i >= 0 {
		a.library.History = slices.Delete(a.library.History, i, i+1)
		a.library.updateLast()
		a.save()
	}
}

func (a *app) openBrowserChapter(chapter item, services ui.Services) {
	a.openBrowserProgress(progress{SourceID: a.comicSource, Comic: a.comic, Chapter: chapter, Page: -1}, services)
}

func (a *app) openBrowserProgress(p progress, services ui.Services) {
	address, err := resolveURL(nil, p.Chapter.URL)
	if err != nil {
		a.setStatus("无法打开原站："+err.Error(), true)
		return
	}
	if i := a.library.historyIndex(p.SourceID, p.Comic.URL); i >= 0 {
		saved := a.library.History[i]
		if saved.Page >= 0 && saved.Chapter.URL == p.Chapter.URL {
			p = saved
		}
	}
	a.recordProgress(p)
	services.OpenURL(address.String())
}

type catalogSection int

const (
	searchSection catalogSection = iota
	favoritesSection
	historySection
)

func (s catalogSection) title() string {
	switch s {
	case favoritesSection:
		return "收藏"
	case historySection:
		return "阅读历史"
	default:
		return "搜索"
	}
}

func (a *app) navigate(section catalogSection) {
	a.backToCatalog()
	a.section = section
}

func (a *app) catalogHeader(c *ui.Context) {
	ui.Column(c).Background(c.Theme().Surface).Padding(10, 12, 8).Gap(8).Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			openMenu := func() {
				a.sourcePicker = true
				if a.cancel == nil {
					a.setStatus("", false)
				}
			}
			if a.appIcon != nil {
				ui.ButtonBase(c).Label("菜单").Tooltip("菜单").Size(44, 44).Center().OnClick(openMenu).
					Children(func() { ui.Image(c, a.appIcon).Size(42, 42).Fit(ui.Contain) })
			} else {
				ui.Button(c, "菜单").OnClick(openMenu)
			}
			ui.Text(c, appName).FontSize(24).Bold()
		})
		a.catalogNav(c, true)
	})
}

func (a *app) brandView(c *ui.Context) ui.Element {
	return ui.Row(c).Gap(9).AlignItems(ui.Center).Children(func() {
		if a.appIcon != nil {
			ui.Image(c, a.appIcon).Size(44, 44).Shrink(0).Fit(ui.Contain).Label("麦依应用图标")
		}
		ui.Text(c, appName).FontSize(22).Bold().Grow(1).MinWidth(0).SingleLine()
	})
}

func (a *app) catalogNav(c *ui.Context, compact bool) {
	ui.Row(c).Gap(6).Children(func() {
		order := []catalogSection{searchSection, favoritesSection, historySection}
		if compact {
			order = []catalogSection{historySection, favoritesSection, searchSection}
		}
		for _, section := range order {
			label := section.title()
			if compact && section == historySection {
				label = "历史"
			} else if section == searchSection {
				label = "找漫画"
			}
			if a.section == section {
				ui.PrimaryButton(c, label).Grow(1).OnClick(func() { a.navigate(section) })
			} else {
				ui.Button(c, label).Grow(1).OnClick(func() { a.navigate(section) })
			}
		}
	})
}

func (a *app) libraryView(c *ui.Context, compact bool) {
	section, horizontal := a.section, float32(28)
	minWidth, height := float32(140), float32(256)
	if compact {
		horizontal, minWidth, height = 12, 99, 206
	} else {
		ui.Text(c, section.title()).FontSize(29).Bold().Padding(23, horizontal, 10)
	}

	if section == favoritesSection {
		if len(a.library.Favorites) == 0 {
			ui.Text(c, "还没有收藏。打开漫画的章节列表即可收藏。").
				Wrap().TextColor(c.Theme().TextMuted).Padding(16, horizontal)
			return
		}
		c.Key(favoritesSection)
		ui.GridView(c, nil, len(a.library.Favorites), minWidth, height, func(i int) {
			entry := a.library.Favorites[i]
			a.libraryCard(c, entry.Comic, entry.SourceID, "", func() { a.openComic(entry.Comic, entry.SourceID, nil) },
				func(menu *ui.Menu) {
					if menu.Item("取消收藏").Danger().Chosen() {
						a.removeFavorite(entry.SourceID, entry.Comic.URL)
					}
				})
		}).Grow(1).Basis(0).Padding(8, horizontal, 20)
		return
	}
	if len(a.library.History) == 0 {
		ui.Text(c, "还没有阅读记录。打开章节后会自动保存。").
			Wrap().TextColor(c.Theme().TextMuted).Padding(16, horizontal)
		return
	}
	services := c.Services()
	c.Key(historySection)
	ui.GridView(c, nil, len(a.library.History), minWidth, height, func(i int) {
		entry := a.library.History[i]
		a.libraryCard(c, entry.Comic, entry.SourceID, entry.description(), func() { a.resume(entry, services) },
			func(menu *ui.Menu) {
				if src := a.sourceByID(entry.SourceID); src.ID != "" && !src.readsInBrowser() {
					if menu.Item("在应用内阅读").Chosen() {
						a.resumeInApp(entry)
					}
				}
				if menu.Item("在原站打开").Chosen() {
					a.backToCatalog()
					a.openBrowserProgress(entry, services)
				}
				if menu.Item("删除记录").Danger().Chosen() {
					a.removeHistory(entry)
				}
			})
	}).Grow(1).Basis(0).Padding(8, horizontal, 20)
}

func (a *app) libraryCard(c *ui.Context, comic item, id, position string, open func(), buildMenu func(*ui.Menu)) {
	theme := c.Theme()
	comic.Cover = a.coverForComic(id, comic)
	ui.Column(c).Fill().Radius(10).Background(theme.Surface).Border(1, theme.Border).Children(func() {
		ui.ButtonBase(c).Label(comic.Title).Grow(1).Basis(0).Radius(9).Clip().OnClick(open).
			PopoverMenu(buildMenu).Children(func() {
			ui.Column(c).Fill().Children(func() {
				ui.Box(c).Grow(1).Basis(0).WidthPercent(100).Clip().Background(theme.Border).
					Children(func() { comicCoverView(c, comic, 640) })
				ui.Column(c).Padding(7, 8, 9).Gap(3).Children(func() {
					ui.Text(c, comic.Title).FontSize(13).Bold().Wrap().MaxLines(2)
					ui.Text(c, a.sourceLabel(id)).FontSize(11).SingleLine().TextColor(theme.TextMuted)
					if position != "" {
						ui.Text(c, position).FontSize(10).SingleLine().TextColor(theme.TextMuted)
					}
				})
			})
		})
	})
}

func comicCoverView(c *ui.Context, comic item, maxSide int) {
	if comic.Cover != "" {
		picture, err := netimage.Load(c, comic.Cover, netimage.Options{MaxSide: maxSide})
		if picture != nil {
			ui.Image(c, picture).Fill().Fit(ui.Cover).Description(comic.Title + "封面")
			return
		}
		if err == nil {
			return
		}
	}
	ui.Box(c).Fill().Center().Children(func() {
		ui.Text(c, "暂无封面").FontSize(11).TextColor(c.Theme().TextMuted)
	})
}

type coverKey struct {
	sourceID string
	comicURL string
}

func (a *app) coverForComic(id string, comic item) string {
	if comic.Cover != "" {
		return comic.Cover
	}
	cover := a.comicCovers[coverKey{id, comic.URL}]
	if cover == "" {
		a.requestComicCover(id, comic)
	}
	return cover
}

func (a *app) requestComicCover(id string, comic item) {
	const maxCoverRequests = 3
	if a.win == nil || a.coverLoading >= maxCoverRequests || comic.URL == "" {
		return
	}
	key := coverKey{id, comic.URL}
	if _, requested := a.comicCovers[key]; requested {
		return
	}
	src := a.sourceByID(id)
	if src.ID == "" {
		return
	}
	if a.comicCovers == nil {
		a.comicCovers = make(map[coverKey]string)
	}
	a.comicCovers[key] = ""
	a.coverLoading++
	win, client := a.win, a.client
	go func() {
		detail, err := src.comicDetails(context.Background(), client, comic)
		win.Update(func() {
			a.coverLoading--
			if err == nil {
				a.storeComicCover(key, detail.Cover)
			}
		})
	}()
}

func (a *app) storeComicCover(key coverKey, cover string) {
	if cover == "" {
		return
	}
	if a.comicCovers == nil {
		a.comicCovers = make(map[coverKey]string)
	}
	a.comicCovers[key] = cover
	changed := false
	if i := a.library.favoriteIndex(key.sourceID, key.comicURL); i >= 0 && a.library.Favorites[i].Comic.Cover == "" {
		a.library.Favorites[i].Comic.Cover = cover
		changed = true
	}
	if i := a.library.historyIndex(key.sourceID, key.comicURL); i >= 0 && a.library.History[i].Comic.Cover == "" {
		a.library.History[i].Comic.Cover = cover
		a.library.updateLast()
		changed = true
	}
	if changed {
		a.save()
	}
}

func (a *app) sourceLabel(id string) string {
	if src := a.sourceByID(id); src.ID != "" {
		return src.Name
	}
	return "图源未导入"
}
