package main

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/egoist/mygo/ui"
)

const copyMangaSourceID = "cimoc-COPYMH"

var copyMangaRankPeriods = [...]struct{ slug, label string }{
	{"day", "日榜"}, {"week", "周榜"}, {"month", "月榜"}, {"total", "总榜"},
}

var copyMangaRankRule = rule{
	Format: "html",
	Items:  "ul.ranking-all .ranking-all-box",
	Fields: map[string]string{
		"title": ".ranking-all-topThree-txt a[href^='/comic/']",
		"url":   ".ranking-all-topThree-txt a[href^='/comic/']@href",
		"cover": "img[data-src]@data-src",
	},
}

type copyMangaRankState struct {
	period int
	boards [len(copyMangaRankPeriods)]copyMangaRankBoard
}

type copyMangaRankBoard struct {
	items  []searchMatch
	scroll ui.ListState
}

func fetchCopyMangaRank(ctx context.Context, client *http.Client, period string) ([]item, error) {
	params := url.Values{"type": {"male"}, "table": {period}}
	body, final, err := fetchCopyMangaWeb(ctx, client, "/rank?"+params.Encode())
	if err != nil {
		return nil, err
	}
	if final.Path != "/rank" || final.Query().Get("type") != "male" || final.Query().Get("table") != period {
		return nil, errors.New("拷贝漫画未返回所选男频榜单")
	}
	items, _, err := copyMangaRankRule.parsePage(body, final, false)
	if err != nil || len(items) == 0 {
		return nil, errors.New("拷贝漫画男频榜单暂时无法读取")
	}
	return items, nil
}

func (a *app) selectCopyMangaRank(period int) {
	a.stop()
	a.setStatus("", false)
	a.copyRank.period = period
	if len(a.copyRank.boards[period].items) == 0 {
		a.loadCopyMangaRank(period)
	}
}

func (a *app) loadCopyMangaRank(period int) {
	src, client := a.sourceByID(copyMangaSourceID), a.client
	if src.ID == "" {
		return
	}
	choice := copyMangaRankPeriods[period]
	a.start("正在加载男频"+choice.label+"…", func(ctx context.Context) (func(), error) {
		comics, err := fetchCopyMangaRank(ctx, client, choice.slug)
		if err != nil {
			return nil, err
		}
		results := make([]searchMatch, len(comics))
		for i, comic := range comics {
			results[i] = searchMatch{Item: comic, SourceID: src.ID, SourceName: src.Name}
		}
		return func() { a.copyRank.boards[period].items = results }, nil
	})
}

func (a *app) copyMangaRankView(c *ui.Context, compact bool) {
	horizontal := float32(28)
	if compact {
		horizontal = 18
	}
	if a.sourceByID(copyMangaSourceID).ID == "" {
		ui.Text(c, "导入拷贝漫画图源后即可查看男频榜。").Wrap().Padding(18, horizontal)
		return
	}
	ui.Column(c).Padding(18, horizontal, 12).Gap(10).Shrink(0).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, "拷贝漫画男频排行榜").FontSize(22).Bold().Grow(1).MinWidth(0).Wrap()
			ui.Button(c, "刷新").Shrink(0).Disabled(a.cancel != nil).OnClick(func() { a.loadCopyMangaRank(a.copyRank.period) })
		})
		labels := make([]string, 0, len(copyMangaRankPeriods))
		for _, entry := range copyMangaRankPeriods {
			labels = append(labels, entry.label)
		}
		period := a.copyRank.period
		ui.Segmented(c, &period, labels...).Label("榜单周期").FillEqually().OnChange(func() {
			a.selectCopyMangaRank(period)
		})
	})
	board := &a.copyRank.boards[a.copyRank.period]
	if len(board.items) == 0 {
		return
	}
	c.Key(a.copyRank.period)
	a.searchResultList(c, board.items, horizontal, &board.scroll, true)
}
