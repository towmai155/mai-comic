package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/netimage"
)

const appName = "mai comic"

// Limit the texture uploaded when a tall comic page enters the phone viewport.
const phoneReaderMaxImageSide = 2560

type app struct {
	win          *mygo.Window
	appIcon      *ui.Bitmap
	client       *http.Client
	statePath    string
	library      library
	cancel       context.CancelFunc
	status       string
	failed       bool
	phone        bool
	section      catalogSection
	comicCovers  map[coverKey]string
	coverLoading int

	sourcePicker      bool
	reportOpen        bool
	importURL         string
	query             string
	searchedQuery     string
	sourceID          string
	results           []searchMatch
	searched          bool
	copyRank          copyMangaRankState
	comicSource       string
	comic             item
	detail            item
	summaryOpen       bool
	chapters          []item
	chapterCatalog    ui.ListState
	sourceNewestFirst bool
	chapter           int
	pages             []item
	pageRatios        []float32
	reader            ui.ListState
	nativeReader      nativeReading
	firstPage         int
	seekPage          float64
	seekingPage       bool
	readerControls    bool
	readerSettings    bool
	lastPageReady     bool
	autoNextTried     bool
}

func main() {
	mygo.App.SetName(appName)
	client := &http.Client{Timeout: 20 * time.Second}
	netimage.SetClient(&http.Client{Timeout: client.Timeout, Transport: comicImageTransport{}})
	a := &app{client: client, chapter: -1, phone: runtime.GOOS == "android" || runtime.GOOS == "ios"}
	if a.phone {
		a.section = historySection
	}
	mygo.App.WhenReady(func() {
		icon, err := loadAppIcon()
		if err != nil {
			log.Printf("加载应用图标：%v", err)
		}
		a.appIcon = icon
		dir, err := mygo.App.Path(mygo.PathAppData)
		if err == nil {
			// Keep the existing library and window state after renaming the app.
			dir = filepath.Join(dir, "mygo-manga")
			err = os.MkdirAll(dir, 0o755)
		}
		if err == nil {
			mygo.App.SetPath(mygo.PathUserData, dir)
			a.statePath = filepath.Join(dir, "library.json")
			a.library, err = readLibrary(a.statePath)
		}
		if err != nil {
			a.setStatus("无法加载本地记录："+err.Error(), true)
		}
		if a.library.Appearance != "" {
			mygo.Theme.SetSource(a.library.Appearance)
		}
		a.win = mygo.NewWindow(mygo.WindowOptions{
			Title: appName, Width: 1040, Height: 760, MinWidth: 720, MinHeight: 490,
			StateKey: "reader", Content: ui.View(a.view),
		})
		a.win.OnClose(func(_ *mygo.CloseEvent) {
			a.stop()
			a.remember()
			a.closeNativeChapter()
		})
		mygo.App.OnDidResignActive(a.remember)
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

func loadAppIcon() (*ui.Bitmap, error) {
	dir, err := mygo.App.Path(mygo.PathResources)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "icon.png"))
	if err != nil {
		return nil, err
	}
	return ui.DecodeBitmapScaled(data, 192)
}

func (a *app) view(c *ui.Context) {
	// The native Android appearance override is only available from API 31.
	// Apply the palette to the view when the host has a different appearance.
	switch a.library.Appearance {
	case mygo.ThemeDark:
		if !c.Theme().Dark {
			c.SetTheme(ui.DarkTheme().WithPreferences(c.Preferences()))
		}
	case mygo.ThemeLight:
		if c.Theme().Dark {
			c.SetTheme(ui.LightTheme().WithPreferences(c.Preferences()))
		}
	}
	theme := c.Theme()
	width, _ := c.Size()
	compact := a.phone || width < 680
	drawerOpen := compact && a.sourcePicker
	if a.reportOpen {
		a.importReportView(c)
		return
	}
	if len(a.pages) > 0 {
		c.OnShortcut(0, ui.KeyBack, a.readerBack)
		c.OnShortcut(0, ui.KeyEscape, a.readerBack)
		ui.Column(c).Fill().Background(theme.Background).Children(func() {
			a.statusView(c)
			a.readView(c, compact, a.openChapter)
		})
		return
	}
	if !drawerOpen && a.comic.URL != "" {
		if !a.summaryOpen {
			c.OnShortcut(0, ui.KeyBack, a.backToCatalog)
		}
	} else if !drawerOpen && a.section != searchSection {
		c.OnShortcut(0, ui.KeyBack, func() { a.navigate(searchSection) })
	}
	if compact {
		ui.Column(c).Fill().Background(theme.Background).Children(func() {
			if a.comic.URL == "" {
				a.catalogHeader(c)
			}
			a.content(c, true)
		})
		if drawerOpen {
			a.sourceDrawer(c, max(1, min(310, width-42)))
		}
		return
	}
	ui.Row(c).Fill().Background(theme.Background).Children(func() {
		ui.Column(c).Width(264).HeightPercent(100).Shrink(0).Background(theme.Surface).Children(func() {
			ui.Scroll(c).Grow(1).Padding(24, 18).Gap(12).Children(func() {
				a.brandView(c)
				a.catalogNav(c, false)
				a.appearanceView(c)
				a.recentView(c)
				ui.Text(c, "图源").FontSize(15).Bold().Padding(10, 0, 0)
				a.sourcesView(c)
			})
		})
		ui.Column(c).Grow(1).Basis(0).HeightPercent(100).Children(func() {
			a.content(c, false)
		})
	})
}

func (a *app) content(c *ui.Context, compact bool) {
	if !compact || !a.sourcePicker {
		a.statusView(c)
	}
	switch {
	case a.comic.URL != "":
		a.chapterView(c, compact)
	case a.section == copyMangaRankSection:
		a.copyMangaRankView(c, compact)
	case a.section != searchSection:
		a.libraryView(c, compact)
	default:
		a.searchView(c, compact)
	}
}

func (a *app) statusView(c *ui.Context) {
	if a.status == "" {
		return
	}
	theme := c.Theme()
	color := theme.TextMuted
	if a.failed {
		color = theme.Text
	}
	ui.Text(c, a.status).Wrap().TextColor(color).Background(theme.Surface).Padding(10, 20)
}

func (a *app) sourceDrawer(c *ui.Context, width float32) {
	top, _, bottom, _ := c.SafeArea()
	ui.Overlay(c, func() {
		backdrop := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Background(ui.RGBA(0, 0, 0, .38)).Modal()
		backdrop.Children(func() {
			panel := ui.Column(c).Width(width).FillHeight().Padding(top, 0, bottom, 0).
				Background(c.Theme().Background).Role(ui.RoleDialog).Label("菜单")
			panel.Children(func() {
				ui.Row(c).AlignItems(ui.Center).Padding(12, 18).Children(func() {
					a.brandView(c).Grow(1).Basis(0).MinWidth(0)
					ui.Button(c, "关闭").OnClick(a.hideSources)
				})
				a.statusView(c)
				ui.Scroll(c).Grow(1).Basis(0).Padding(8, 18, 24).Gap(12).
					Children(func() {
						a.appearanceView(c)
						ui.Text(c, "图源管理").FontSize(16).Bold().Padding(9, 0, 0)
						a.sourcesView(c)
					})
			})
			if panel.PressedOutside() || backdrop.OverlayShortcut(0, ui.KeyBack) || backdrop.OverlayShortcut(0, ui.KeyEscape) {
				a.hideSources()
			}
		})
	})
}

func (a *app) appearanceView(c *ui.Context) {
	modes := [...]mygo.ThemeSource{mygo.ThemeLight, mygo.ThemeDark, mygo.ThemeSystem}
	selected := slices.Index(modes[:], a.library.Appearance)
	if selected < 0 {
		selected = 2
	}
	ui.Column(c).Gap(6).Children(func() {
		ui.Text(c, "界面模式").FontSize(15).Bold()
		ui.Segmented(c, &selected, "白天", "夜晚", "跟随系统").
			Label("界面模式").FillEqually().OnChange(func() {
			a.setAppearance(modes[selected])
		})
	})
}

func (a *app) setAppearance(mode mygo.ThemeSource) {
	if a.library.Appearance == mode {
		return
	}
	a.library.Appearance = mode
	if mygo.App.IsReady() {
		mygo.Theme.SetSource(mode)
	}
	a.save()
}

func (a *app) sourcesView(c *ui.Context) {
	ui.TextInput(c, &a.importURL).Label("图源配置 URL").Placeholder("https://…/sources.json").
		OnSubmit(a.importFromURL)
	ui.Row(c).Gap(8).Children(func() {
		ui.Button(c, "导入图源").Grow(1).Disabled(a.cancel != nil).OnClick(a.importFromURL)
		ui.Button(c, "刷新图源").Grow(1).Disabled(a.cancel != nil).OnClick(a.refreshSources)
	})
	if len(a.library.CimocReport) > 0 {
		ui.Button(c, "查看导入明细").OnClick(func() { a.reportOpen = true })
	}
	if len(a.library.Sources) > 0 {
		if a.sourceID == "" {
			ui.PrimaryButton(c, "全部图源").OnClick(func() { a.selectSource("") })
		} else {
			ui.Button(c, "全部图源").OnClick(func() { a.selectSource("") })
		}
	}
	for _, src := range a.library.Sources {
		c.Key(src.ID)
		if src.ID == a.sourceID {
			ui.PrimaryButton(c, src.Name).OnClick(func() { a.selectSource(src.ID) })
		} else {
			ui.Button(c, src.Name).OnClick(func() { a.selectSource(src.ID) })
		}
	}
}

func (a *app) importReportView(c *ui.Context) {
	closeReport := func() { a.reportOpen = false }
	services := c.Services()
	report := a.library.CimocReport
	c.OnShortcut(0, ui.KeyBack, closeReport)
	ui.Column(c).Fill().Background(c.Theme().Background).Children(func() {
		ui.Column(c).Padding(18).Gap(10).Children(func() {
			ui.Button(c, "返回").AlignSelf(ui.Start).OnClick(closeReport)
			ui.Textf(c, "%d 条配置未导入", len(report)).FontSize(22).Bold()
		})
		ui.List(c, nil, len(report), func(i int) {
			entry := report[i]
			ui.Column(c).Padding(10, 12).Gap(5).Background(c.Theme().Surface).Children(func() {
				ui.Text(c, entry.ID).Wrap().Bold()
				ui.Text(c, entry.Reason).Wrap().TextColor(c.Theme().TextMuted)
				if website := cimocWebsite(entry.ID, entry.Website); website != "" {
					ui.Button(c, "打开原站").AlignSelf(ui.Start).OnClick(func() { services.OpenURL(website) })
				}
			})
		}).Grow(1).Basis(0).Gap(8).Padding(0, 18, 18)
	})
}

func (a *app) recentView(c *ui.Context) {
	if last := a.library.Last; last != nil {
		services := c.Services()
		ui.Text(c, "上次阅读").FontSize(15).Bold().Padding(14, 0, 0)
		ui.Text(c, last.Comic.Title).Wrap().MaxLines(2)
		ui.Text(c, last.description()).Wrap().TextColor(c.Theme().TextMuted)
		ui.Button(c, "继续阅读").OnClick(func() {
			a.remember()
			a.resume(*a.library.Last, services)
		})
	}
}

func (a *app) hideSources() {
	a.sourcePicker = false
	if a.failed {
		a.setStatus("", false)
	}
}

func (a *app) searchView(c *ui.Context, compact bool) {
	horizontal := float32(28)
	if compact {
		horizontal = 18
	}
	ui.Column(c).Padding(23, horizontal, 14).Gap(11).Children(func() {
		ui.Text(c, "搜索漫画").FontSize(29).Bold()
		if compact && len(a.library.Sources) > 0 {
			name := "全部图源"
			if src := a.sourceByID(a.sourceID); src.ID != "" {
				name = src.Name
			}
			ui.Text(c, name).TextColor(c.Theme().TextMuted)
		}
		ui.Row(c).Gap(10).Children(func() {
			ui.TextInput(c, &a.query).Label("漫画名称").Placeholder("输入漫画名称").
				Grow(1).OnSubmit(a.search)
			ui.PrimaryButton(c, "搜索").Disabled(len(a.library.Sources) == 0 || a.cancel != nil).OnClick(a.search)
		})
		if ui.Checkbox(c, &a.library.StrictSearch, "严格搜索").Changed() {
			a.save()
		}
		if compact {
			a.recentView(c)
		}
	})
	results := a.results
	if a.library.StrictSearch {
		results = exactTitleMatches(results, a.searchedQuery)
	}
	if len(results) == 0 {
		message := "输入关键词搜索漫画。"
		switch {
		case len(a.library.Sources) == 0:
			message = "先粘贴图源配置 URL，再导入图源。"
			if compact {
				message = "打开左上角菜单，导入图源配置。"
			}
		case a.searched && a.library.StrictSearch:
			message = "没有找到标题完全匹配的漫画。可关闭严格搜索查看更多结果。"
		case a.searched:
			message = "没有找到漫画。试试其他关键词或图源。"
		}
		ui.Text(c, message).Wrap().TextColor(c.Theme().TextMuted).Padding(5, horizontal)
		return
	}
	a.searchResultList(c, results, horizontal, nil, false)
}

func (a *app) searchResultList(c *ui.Context, results []searchMatch, horizontal float32, state *ui.ListState, ranked bool) {
	ui.List(c, state, len(results), func(i int) {
		result := results[i]
		comic := result.Item
		comic.Cover = a.coverForComic(result.SourceID, comic)
		sourceName := result.SourceName
		if a.sourceByID(result.SourceID).readsInBrowser() {
			sourceName += " · 浏览器阅读"
		}
		theme := c.Theme()
		ui.ButtonBase(c).Label(comic.Title+" · "+sourceName).WidthPercent(100).Padding(7, 10).
			Radius(8).Background(theme.Surface).Border(1, theme.Border).Cursor(ui.CursorPointer).
			OnClick(func() { a.openComic(comic, result.SourceID, nil) }).Children(func() {
			ui.Row(c).WidthPercent(100).AlignItems(ui.Center).Gap(12).Children(func() {
				if ranked {
					ui.Textf(c, "%d", i+1).Bold().TextColor(theme.TextMuted).Width(24).Shrink(0).TextAlign(ui.Center)
				}
				ui.Box(c).Size(54, 76).Shrink(0).Radius(5).Clip().Background(theme.Border).
					Children(func() { comicCoverView(c, comic, 180) })
				ui.Column(c).Grow(1).Basis(0).Gap(4).Children(func() {
					ui.Text(c, comic.Title).FontSize(15).Bold().Wrap().MaxLines(2)
					ui.Text(c, sourceName).FontSize(12).SingleLine().TextColor(theme.TextMuted)
				})
			})
		})
	}).Grow(1).Basis(0).Padding(5, horizontal, 24).Gap(7)
}

func (a *app) chapterView(c *ui.Context, compact bool) {
	src := a.sourceByID(a.comicSource)
	browser := src.readsInBrowser()
	hasParser := src.chapterParser() != nil
	horizontal := float32(28)
	if compact {
		horizontal = 18
	}
	ui.Column(c).Shrink(0).Padding(23, horizontal, 16).Gap(10).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			backLabel := "返回" + a.section.title()
			if a.section == copyMangaRankSection {
				backLabel = "返回榜单"
			}
			ui.Button(c, backLabel).OnClick(a.backToCatalog)
			ui.Box(c).Grow(1)
			label := "收藏漫画"
			if a.library.favoriteIndex(a.comicSource, a.comic.URL) >= 0 {
				label = "取消收藏"
			}
			ui.Button(c, label).OnClick(a.toggleFavorite)
		})
		ui.Text(c, a.detail.Title).FontSize(25).Bold().Wrap().MaxLines(2)
		if src.ID != "" {
			ui.Text(c, src.Name).TextColor(c.Theme().TextMuted)
		}
		if a.detail.Summary != "" {
			ui.Text(c, a.detail.Summary).Wrap().MaxLines(4).TextColor(c.Theme().TextMuted)
			ui.Button(c, "查看完整简介").AlignSelf(ui.Start).OnClick(func() { a.summaryOpen = true })
		}
		label := "章节"
		if browser {
			label = "章节 · 在浏览器打开"
		}
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, label).FontSize(16).Bold().Grow(1).MinWidth(0).Wrap()
			if len(a.chapters) > 1 {
				order := 0
				if a.library.ChaptersDescending {
					order = 1
				}
				ui.Segmented(c, &order, "正序", "倒序").Label("章节排序").Shrink(0).OnChange(func() {
					a.library.ChaptersDescending = order == 1
					a.chapterCatalog.ScrollTo(0, ui.Start)
					a.save()
				})
			}
		})
	})
	a.comicSummaryDialog(c)
	if len(a.chapters) == 0 {
		ui.Text(c, "这个图源没有返回章节。").TextColor(c.Theme().TextMuted).Padding(0, horizontal)
		return
	}
	services := c.Services()
	reverse := a.library.ChaptersDescending != a.sourceNewestFirst
	theme := c.Theme()
	const columns = 3
	rows := (len(a.chapters) + columns - 1) / columns
	ui.List(c, &a.chapterCatalog, rows, func(row int) {
		ui.Grid(c).Columns(columns).Gap(10).Children(func() {
			for position := row * columns; position < min((row+1)*columns, len(a.chapters)); position++ {
				i := position
				if reverse {
					i = len(a.chapters) - 1 - position
				}
				chapter := a.chapters[i]
				button := ui.ButtonBase(c.Key(i)).Label(chapter.Title).Height(44).MinWidth(0).Padding(0, 8).
					Radius(22).Background(theme.Surface).Border(1, theme.Border).Center().Clip().
					OnClick(func() {
						if browser {
							a.openBrowserChapter(chapter, services)
							return
						}
						a.openChapter(i)
					})
				if hasParser {
					button.PopoverMenu(func(menu *ui.Menu) {
						if menu.Item("在原站打开").Chosen() {
							a.openBrowserChapter(chapter, services)
						}
					})
				}
				button.Children(func() {
					ui.Text(c, chapter.Title).WidthPercent(100).SingleLine().FontSize(14).TextAlign(ui.Center)
				})
			}
		})
	}).Grow(1).Basis(0).Padding(0, horizontal, 24).Gap(10)
}

func (a *app) comicSummaryDialog(c *ui.Context) {
	if !a.summaryOpen {
		return
	}
	width, height := c.Size()
	panel := ui.Modal(c, &a.summaryOpen, func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, "作品简介").FontSize(18).Bold().Grow(1)
			ui.Button(c, "关闭").OnClick(func() { a.summaryOpen = false })
		})
		ui.Scroll(c).Width(min(480, max(1, width-84))).MaxHeight(min(360, max(1, height-160))).Children(func() {
			ui.Text(c, a.detail.Summary).Wrap()
		})
	})
	if panel.OverlayShortcut(0, ui.KeyBack) {
		a.summaryOpen = false
	}
}

func (a *app) readView(c *ui.Context, compact bool, openChapter func(int)) {
	if a.nativeReader.session == nil {
		first, last := a.reader.Visible()
		if last >= first {
			a.firstPage = min(first, len(a.pages)-1)
		}
	}
	ui.Column(c).Grow(1).Basis(0).Children(func() {
		if !a.presentNativeReader(c) {
			a.readPages(c, compact, openChapter)
		}
		if a.readerControls {
			a.readToolbar(c, compact, openChapter)
			a.readProgress(c, len(a.pages))
		}
	})
}

func (a *app) readToolbar(c *ui.Context, compact bool, openChapter func(int)) {
	top, right, _, left := c.SafeArea()
	previous, next := a.chapterNeighbor(a.chapter, -1), a.chapterNeighbor(a.chapter, 1)
	ui.Column(c).Absolute().Top(0).Left(0).Right(0).
		Padding(13+top, 18+right, 13, 18+left).Gap(8).Background(c.Theme().Surface).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Button(c, "章节列表").OnClick(a.backToChapters)
			if compact {
				ui.Box(c).Grow(1).MinWidth(0)
			} else {
				ui.Text(c, a.chapters[a.chapter].Title).Bold().Grow(1).MinWidth(0).SingleLine()
			}
			ui.Button(c, "上一章").Disabled(previous < 0 || a.cancel != nil).
				OnClick(func() { openChapter(previous) })
			ui.Button(c, "下一章").Disabled(next < 0 || a.cancel != nil).
				OnClick(func() { openChapter(next) })
			settings := ui.Button(c, "设置").OnClick(func() { a.readerSettings = !a.readerSettings })
			ui.Popover(c, settings, &a.readerSettings, func() { a.appearanceView(c) })
		})
		title := a.detail.Title
		if compact {
			title = a.chapters[a.chapter].Title
		}
		if title != "" {
			ui.Text(c, title).SingleLine().TextColor(c.Theme().TextMuted)
		}
	})
}

func (a *app) readPages(c *ui.Context, compact bool, openChapter func(int)) {
	viewport, height := c.Size()
	width := max(1, viewport-24)
	if !compact {
		width = min(760, width)
	}
	chapter, pageCount := a.chapter, len(a.pages)
	next := a.chapterNeighbor(chapter, 1)
	imageLimit := 4096
	if a.phone {
		imageLimit = phoneReaderMaxImageSide
	}
	rowCount := pageCount
	if a.phone && next >= 0 {
		rowCount++
		// The extra row is visible only once scrolling reaches the bottom
		// of the last image, even for pages much taller than the screen.
		a.reader.EndReached = func(int) {
			if a.markChapterEnd(chapter) {
				openChapter(next)
			}
		}
	}
	c.Key(a.chapters[chapter].URL)
	ui.List(c, &a.reader, rowCount, func(i int) {
		if i == pageCount {
			ui.Column(c).Padding(12).Gap(7).Center().Children(func() {
				ui.Text(c, "下一章 · "+a.chapters[next].Title).Wrap().TextColor(c.Theme().TextMuted)
				ui.Button(c, "打开下一章").Disabled(a.cancel != nil).
					OnClick(func() { openChapter(next) })
			})
			return
		}
		page := a.pages[i]
		row := ui.Column(c).AlignItems(ui.Center)
		finalPage := i == pageCount-1
		if finalPage && (pageCount > 1 || rowCount > pageCount) {
			// Leave room to align even a short last page at the top of the reader.
			row.MinHeight(height)
		}
		row.Children(func() {
			picture, err := netimage.Load(c, page.URL, netimage.Options{MaxSide: imageLimit})
			w, h := ui.ImageSize(picture)
			if finalPage {
				a.lastPageReady = w > 0 && h > 0
			}
			if w > 0 && h > 0 {
				a.pageRatios[i] = h / w
				ui.Image(c, picture).Size(width, width*a.pageRatios[i]).Fit(ui.Contain).
					Description(fmt.Sprintf("第 %d 页", i+1))
				return
			}
			ratio := a.pageRatios[i]
			if ratio == 0 {
				ratio = 1.4
			}
			ui.Column(c).Width(width).Height(width * ratio).Center().Gap(10).Background(c.Theme().Surface).Children(func() {
				if err == nil {
					ui.Textf(c, "正在加载第 %d 页", i+1).TextColor(c.Theme().TextMuted)
					return
				}
				ui.Textf(c, "第 %d 页加载失败", i+1)
				ui.Button(c, "重试").OnClick(func() { netimage.Forget(page.URL) })
			})
		})
	}).Grow(1).Basis(0).Padding(0, 8, 16).Gap(8).
		Label("漫画阅读区").Description("点按显示或收起阅读控件").Focusable().OnClick(a.toggleReaderControls)
}

func (a *app) toggleReaderControls() {
	if a.readerControls {
		a.hideReaderControls()
		return
	}
	a.readerControls = true
}

func (a *app) hideReaderControls() {
	a.readerControls, a.readerSettings, a.seekingPage = false, false, false
}

func (a *app) readerBack() {
	if a.readerControls {
		a.hideReaderControls()
		return
	}
	a.backToChapters()
}

func (a *app) readProgress(c *ui.Context, pageCount int) {
	_, right, bottom, left := c.SafeArea()
	c.Key("reader-page-progress:" + a.chapters[a.chapter].URL)
	ui.Row(c).Absolute().Bottom(0).Left(0).Right(0).
		AlignItems(ui.Center).Justify(ui.End).Gap(12).Padding(8, 18+right, 8+bottom, 18+left).
		Background(c.Theme().Surface).Children(func() {
		if !a.seekingPage {
			a.seekPage = float64(a.firstPage + 1)
		}
		if pageCount > 1 {
			disabled := a.cancel != nil
			slider := ui.Slider(c, &a.seekPage, 1, float64(pageCount)).Step(1).
				Label("跳页").Grow(1).MinWidth(0).Disabled(disabled)
			button, held := slider.PressedButton()
			pressed := held && button == 0
			if !pressed {
				_, _, dragged := slider.DragEnded()
				if !disabled && (dragged || slider.Clicked() || (!a.seekingPage && slider.Changed())) {
					page := int(a.seekPage) - 1
					a.seekReaderPage(page)
				} else {
					a.seekPage = float64(a.firstPage + 1)
				}
			}
			a.seekingPage = pressed
		}
		ui.Textf(c, "%d / %d 页", int(a.seekPage), pageCount).
			Width(96).Shrink(0).SingleLine().TextAlign(ui.End).TextColor(c.Theme().TextMuted)
	})
}

func (a *app) markChapterEnd(chapter int) bool {
	if !a.phone || a.chapter != chapter || a.chapterNeighbor(chapter, 1) < 0 ||
		a.autoNextTried || a.cancel != nil || !a.lastPageReady {
		return false
	}
	a.autoNextTried = true
	a.firstPage = len(a.pages) - 1
	return true
}

func (a *app) importFromURL() {
	if a.cancel != nil {
		return
	}
	address, client := sourceConfigURL(a.importURL), a.client
	if address == "" {
		a.setStatus("请输入图源配置 URL。", true)
		return
	}
	a.start("正在导入图源…", func(ctx context.Context) (func(), error) {
		result, err := importSources(ctx, client, address)
		if err != nil {
			return nil, err
		}
		return func() {
			a.library.applySourceImport(address, result)
			if len(result.Sources) > 0 {
				a.selectSource("")
			}
			a.importURL = ""
			status := fmt.Sprintf("已导入 %d 个图源。", len(result.Sources))
			if result.Cimoc {
				browser := 0
				for _, src := range result.Sources {
					if src.readsInBrowser() {
						browser++
					}
				}
				status = fmt.Sprintf("已导入 %d 个图源，%d 个应用内读图，%d 个在原站打开章节。",
					len(result.Sources), len(result.Sources)-browser, browser)
			}
			if len(result.Skipped) > 0 {
				status += fmt.Sprintf("另有 %d 条配置未导入，可在图源页查看原因。", len(result.Skipped))
			}
			a.setStatus(status, false)
			a.save()
		}, nil
	})
}

func (a *app) refreshSources() {
	if a.cancel != nil {
		return
	}
	input, client := a.importURL, a.client
	addresses := slices.Clone(a.library.SourceURLs)
	if address := sourceConfigURL(input); address != "" {
		addresses = []string{address}
	}
	if len(addresses) == 0 {
		a.setStatus("请粘贴原图源配置 URL，再点「刷新图源」。刷新成功后会记住地址。", true)
		return
	}
	a.start("正在刷新图源…", func(ctx context.Context) (func(), error) {
		updates := fetchSourceUpdates(ctx, client, addresses)
		return func() {
			a.applySourceRefresh(updates)
			if !a.failed && a.importURL == input {
				a.importURL = ""
			}
		}, nil
	})
}

func (a *app) applySourceRefresh(updates []sourceUpdate) {
	changed := make(map[string]bool)
	successes, failures := 0, 0
	var firstError error
	for _, update := range updates {
		if update.err != nil {
			failures++
			if firstError == nil {
				firstError = update.err
			}
			continue
		}
		successes++
		a.library.applySourceImport(update.address, update.result)
		for _, src := range update.result.Sources {
			changed[src.ID] = true
		}
	}
	if successes == 0 {
		a.setStatus(fmt.Sprintf("刷新图源失败：%v", firstError), true)
		return
	}
	if len(changed) > 0 {
		a.results, a.searched = nil, false
	}
	status := fmt.Sprintf("已刷新 %d 份配置，更新 %d 个图源。", successes, len(changed))
	if failures > 0 {
		status += fmt.Sprintf("另有 %d 份配置刷新失败。", failures)
	}
	a.setStatus(status, failures > 0)
	a.save()
}

func (a *app) selectSource(id string) {
	a.backToSearch()
	a.hideSources()
	a.sourceID = id
	a.results, a.searched = nil, false
}

func (a *app) sourceByID(id string) source {
	for _, src := range a.library.Sources {
		if src.ID == id {
			return src
		}
	}
	return source{}
}

func (a *app) search() {
	if a.cancel != nil || len(a.library.Sources) == 0 {
		return
	}
	query, client := strings.TrimSpace(a.query), a.client
	if query == "" {
		a.setStatus("请输入漫画名称。", true)
		return
	}
	sources := slices.Clone(a.library.Sources)
	label := fmt.Sprintf("正在搜索 %d 个图源…", len(sources))
	if src := a.sourceByID(a.sourceID); src.ID != "" {
		sources = []source{src}
		label = "正在搜索 " + src.Name + "…"
	}
	a.results, a.searched = nil, false
	a.start(label, func(ctx context.Context) (func(), error) {
		results, failures := searchSources(ctx, client, sources, query)
		if len(failures) == len(sources) {
			if len(sources) == 1 {
				return nil, failures[0]
			}
			return nil, fmt.Errorf("%d 个图源均搜索失败。%w", len(failures), failures[0])
		}
		return func() {
			a.results, a.searched = results, true
			a.searchedQuery = query
			if len(failures) > 0 {
				a.setStatus(fmt.Sprintf("%d 个图源搜索失败，已显示其余图源的结果。", len(failures)), false)
			}
		}, nil
	})
}

func (a *app) resume(entry progress, services ui.Services) {
	if entry.Page < 0 {
		a.backToCatalog()
		a.openBrowserProgress(entry, services)
		return
	}
	a.resumeInApp(entry)
}

func (a *app) resumeInApp(entry progress) {
	a.backToCatalog()
	a.openComic(entry.Comic, entry.SourceID, &entry)
}

func (a *app) openComic(comic item, id string, resume *progress) {
	src, client := a.sourceByID(id), a.client
	if src.ID == "" {
		a.setStatus("请重新导入这本漫画的图源。", true)
		return
	}
	a.start("正在加载章节…", func(ctx context.Context) (func(), error) {
		detail, chapters, err := src.loadComic(ctx, client, comic)
		if err != nil {
			return nil, err
		}
		newestFirst := newestChapterFirst(chapters)
		index := -1
		var pages []item
		if resume != nil && !src.readsInBrowser() {
			index = slices.IndexFunc(chapters, func(ch item) bool { return ch.URL == resume.Chapter.URL })
			if index < 0 {
				index = slices.IndexFunc(chapters, func(ch item) bool { return ch.Title == resume.Chapter.Title })
			}
			if index >= 0 {
				pages, err = fetchPages(ctx, src, client, chapters[index])
			}
		}
		return func() {
			if detail.Cover != "" {
				comic.Cover = detail.Cover
			}
			a.comic, a.detail, a.chapters, a.comicSource = comic, detail, chapters, src.ID
			a.sourceNewestFirst = newestFirst
			a.chapterCatalog = ui.ListState{}
			a.storeComicCover(coverKey{src.ID, comic.URL}, comic.Cover)
			if resume == nil || src.readsInBrowser() {
				return
			}
			if index < 0 {
				a.setStatus("上次阅读的章节已找不到，请从列表选择。", true)
			} else if err != nil {
				a.setStatus("无法恢复阅读："+err.Error(), true)
			} else {
				a.showChapter(index, pages, resume.Page)
			}
		}, nil
	})
}

func (a *app) openChapter(index int) {
	if index < 0 || index >= len(a.chapters) || a.cancel != nil {
		return
	}
	a.remember()
	src, client, chapter := a.sourceByID(a.comicSource), a.client, a.chapters[index]
	a.start("正在加载图片列表…", func(ctx context.Context) (func(), error) {
		pages, err := fetchPages(ctx, src, client, chapter)
		return func() {
			page := 0
			if i := a.library.historyIndex(src.ID, a.comic.URL); i >= 0 {
				last := a.library.History[i]
				if last.Chapter.URL == chapter.URL {
					page = max(0, last.Page)
				}
			}
			a.showChapter(index, pages, page)
		}, err
	})
}

func fetchPages(ctx context.Context, src source, client *http.Client, chapter item) ([]item, error) {
	var pages []item
	var err error
	if parse := src.chapterParser(); parse != nil {
		target, urlErr := resolveURL(nil, chapter.URL)
		if urlErr != nil {
			return nil, urlErr
		}
		pages, err = parse(ctx, client, target)
	} else {
		pages, err = src.Pages.run(ctx, client, chapter.URL, src.Pages.URL, false)
	}
	if err == nil && len(pages) == 0 {
		err = errors.New("图源没有返回图片，请检查该章节或图源规则")
	}
	return pages, err
}

func (a *app) showChapter(index int, pages []item, page int) {
	a.chapter, a.pages = index, pages
	a.pageRatios = make([]float32, len(pages))
	a.firstPage = max(0, min(page, len(pages)-1))
	a.hideReaderControls()
	a.lastPageReady, a.autoNextTried = false, false
	a.reader = ui.ListState{}
	a.reader.ScrollTo(a.firstPage, ui.Start)
	a.openNativeChapter()
	a.remember()
}

func (a *app) backToChapters() {
	a.stop()
	a.setStatus("", false)
	a.remember()
	a.closeNativeChapter()
	a.hideReaderControls()
	a.pages, a.pageRatios, a.chapter = nil, nil, -1
}

func (a *app) backToSearch() {
	a.navigate(searchSection)
}

func (a *app) backToCatalog() {
	a.backToChapters()
	a.summaryOpen = false
	a.comic, a.detail, a.chapters, a.comicSource = item{}, item{}, nil, ""
}

func (a *app) remember() {
	if a.chapter < 0 || len(a.pages) == 0 {
		return
	}
	next := progress{SourceID: a.comicSource, Comic: a.comic, Chapter: a.chapters[a.chapter], Page: a.firstPage}
	a.recordProgress(next)
}

func (a *app) start(label string, work func(context.Context) (func(), error)) {
	a.stop()
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.setStatus(label, false)
	win := a.win
	go func() {
		apply, err := work(ctx)
		win.Update(func() {
			if ctx.Err() != nil {
				return
			}
			a.stop()
			if err != nil {
				a.setStatus(err.Error(), true)
				return
			}
			a.setStatus("", false)
			apply()
		})
	}()
}

func (a *app) stop() {
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
}

func (a *app) setStatus(text string, failed bool) {
	a.status, a.failed = text, failed
}

func readLibrary(path string) (library, error) {
	var data library
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return data, nil
	}
	if err != nil {
		return data, err
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return library{}, err
	}
	switch data.Appearance {
	case mygo.ThemeSystem, mygo.ThemeLight, mygo.ThemeDark:
	default:
		data.Appearance = ""
	}
	if len(data.History) == 0 && data.Last != nil {
		data.History = []progress{*data.Last}
	}
	data.updateLast()
	return data, nil
}

func (a *app) save() {
	if a.statePath == "" {
		a.setStatus("本地数据目录不可用，无法保存。", true)
		return
	}
	if err := writeLibrary(a.statePath, a.library); err != nil {
		log.Print(err)
		a.setStatus("保存失败："+err.Error(), true)
	}
}

func writeLibrary(path string, data library) error {
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), "library-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(raw)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
