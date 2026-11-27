package main

import (
	"slices"
	"strings"
)

// Cimoc also stores search servers, CDNs and update mirrors as strings.
// Only known site entries can appear as website links in the import report.
var cimocWebsiteIDs = strings.Fields(`
	ANIMX2 BAINIAN BAOZIMH BAOZIV2 BILIMANHUA BIQUGE BMG BODONGMH BOLEMH BUKA
	CARTOONMAD CCMH CCTUKU CHUIXUE CHUIXUEMH CHUIYAO COPYMH DASHUMANHUA
	DM5 DMZJ DMZJFIX DMZJV2 DongManWu DongManZhiJia DUMANWU DUMANWUORG
	FEIXUEMH GODAMH GoDaMH GUFENG GuFengManHua GuFengWang GUOGUOMH
	HAODUOMAN HAOMAN6 HAOMAN8 HaoManEight HaoManSix HHAAZZ HOTMANGA
	IKANMAN IKANMANTW KOMIIC KUMANWU LAIMH MANGABZ MANGAKAKALOT MANGANEL
	MANHUA90 MANHUADB MANHUATAI MANHUAWU MANHUAYU MANMANMANHUA
	MangaBZManHua ManHuaTai MH50 MH57 MH160 MH360 MH517 MIGU MKZHAN MYCOMIC
	OHMANHUA PUFEI PUFEIMH QIMANHUA QIMANWU QIMIAO QIMIAOMH QIXIMH
	RUMANHUA SHOUJIMH SIXMH SUMANKU TENCENT TTKANMANHUA TUHAO TUHAOMHV2
	U17 VOMIC2 WEBTOON WEBTOONDONGMANMANHUA XINXINMH XMANHUA YEMANMH
	YIMMH YKMH YOUDM YOUMANHUAWU YYMANHUA ZERO
`)

func cimocWebsite(id, address string) string {
	if !slices.Contains(cimocWebsiteIDs, id) {
		return ""
	}
	url, err := resolveURL(nil, address)
	if err != nil || (url.Path != "" && url.Path != "/") || url.RawQuery != "" || url.Fragment != "" {
		return ""
	}
	return url.String()
}
