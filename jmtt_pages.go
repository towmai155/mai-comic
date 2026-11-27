package main

import (
	"bytes"
	"context"
	"crypto/md5"
	"errors"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"

	_ "golang.org/x/image/webp"
)

const jmttStripMarker = "mai-jmtt-strips="

var (
	jmttPhotoPath  = regexp.MustCompile(`^/photo/([0-9]+)(?:/.*)?$`)
	jmttMediaPath  = regexp.MustCompile(`^/media/photos/([0-9]+)/[^/]+$`)
	jmttScrambleID = regexp.MustCompile(`\bvar\s+scramble_id\s*=\s*([0-9]+)\s*;`)
	jmttCDNPrefix  = regexp.MustCompile(`^cdn-msp[0-9]*$`)
)

var jmttPageRule = rule{
	Format: "html", Items: ".scramble-page img[data-original]:not([data-original=''])",
	Fields: map[string]string{"url": "@data-original"},
}

func parseJMTTPages(ctx context.Context, client *http.Client, chapter *url.URL) ([]item, error) {
	if !jmttReaderHost(chapter) || !jmttPhotoPath.MatchString(chapter.Path) {
		return nil, errors.New("禁漫天堂章节地址格式无效")
	}
	body, final, err := fetch(ctx, client, chapter, 8<<20)
	if err != nil {
		return nil, err
	}
	photo := jmttPhotoPath.FindStringSubmatch(final.Path)
	if !jmttReaderHost(final) || len(photo) == 0 {
		return nil, errors.New("禁漫天堂未提供公开阅读页，请在原站打开")
	}
	layout := jmttScrambleID.FindSubmatch(body)
	if len(layout) == 0 {
		return nil, errors.New("禁漫天堂阅读页缺少图片排版参数")
	}
	photoID, photoErr := strconv.Atoi(photo[1])
	scrambleID, scrambleErr := strconv.Atoi(string(layout[1]))
	if photoErr != nil || photoID <= 0 || scrambleErr != nil || scrambleID <= 0 {
		return nil, errors.New("禁漫天堂图片排版参数无效")
	}
	raw, _, err := jmttPageRule.parsePage(body, final, false)
	if err != nil {
		return nil, err
	}
	var pages []item
	seen := make(map[string]bool)
	for _, page := range raw {
		target, err := url.Parse(page.URL)
		if err != nil || !jmttImageHost(target) {
			continue
		}
		media := jmttMediaPath.FindStringSubmatch(target.Path)
		if len(media) == 0 || media[1] != photo[1] {
			continue
		}
		target.Fragment, target.RawFragment = "", ""
		filename := path.Base(target.Path)
		ext := path.Ext(filename)
		if !strings.EqualFold(ext, ".gif") {
			if strips := jmttStripCount(photoID, scrambleID, strings.TrimSuffix(filename, ext)); strips > 0 {
				// netimage keys its cache on the full URL. A fragment carries
				// the local layout without changing the server request.
				target.Fragment = jmttStripMarker + strconv.Itoa(strips)
			}
		}
		address := target.String()
		if seen[address] {
			continue
		}
		seen[address] = true
		pages = append(pages, item{URL: address})
	}
	return pages, nil
}

func jmttReaderHost(address *url.URL) bool {
	return address.Scheme == "https" && isJMTTHost(address.Hostname())
}

func jmttImageHost(address *url.URL) bool {
	prefix, domain, ok := strings.Cut(strings.ToLower(address.Hostname()), ".")
	return address.Scheme == "https" && ok && jmttCDNPrefix.MatchString(prefix) && isJMTTHost(domain)
}

func jmttStripCount(photoID, scrambleID int, basename string) int {
	if photoID < scrambleID {
		return 0
	}
	if photoID < 268850 {
		return 10
	}
	buckets := 10
	if photoID >= 421926 {
		buckets = 8
	}
	const hex = "0123456789abcdef"
	digest := md5.Sum([]byte(strconv.Itoa(photoID) + basename))
	return 2 + 2*(int(hex[digest[len(digest)-1]&0x0f])%buckets)
}

func jmttImageStrips(target *url.URL) int {
	value, ok := strings.CutPrefix(target.Fragment, jmttStripMarker)
	if !ok || !jmttImageHost(target) ||
		!jmttMediaPath.MatchString(target.Path) {
		return 0
	}
	strips, err := strconv.Atoi(value)
	if err != nil || strips < 2 || strips > 20 || strips%2 != 0 {
		return 0
	}
	return strips
}

func restoreJMTTResponse(response *http.Response, strips int) (*http.Response, error) {
	defer response.Body.Close()
	const maxBytes = 16 << 20
	data, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxBytes {
		return nil, errors.New("禁漫天堂图片超过大小限制")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 16_000_000 {
		return nil, errors.New("禁漫天堂图片尺寸超过限制")
	}
	if format == "gif" {
		response.Body = io.NopCloser(bytes.NewReader(data))
		return response, nil
	}
	if config.Height < strips {
		return nil, errors.New("禁漫天堂图片分条数超过图片高度")
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	var output bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := encoder.Encode(&output, restoreJMTTStrips(src, strips)); err != nil {
		return nil, err
	}
	response.Body = io.NopCloser(&output)
	response.ContentLength = int64(output.Len())
	response.Header.Set("Content-Length", strconv.Itoa(output.Len()))
	response.Header.Set("Content-Type", "image/png")
	response.Header.Del("Content-Encoding")
	return response, nil
}

func restoreJMTTStrips(src image.Image, strips int) *image.NRGBA {
	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, width, height))
	srcY, dstY := bounds.Max.Y, 0
	for strip := range strips {
		rows := height / strips
		if strip == 0 {
			rows += height % strips
		}
		srcY -= rows
		draw.Draw(dst, image.Rect(0, dstY, width, dstY+rows), src, image.Pt(bounds.Min.X, srcY), draw.Src)
		dstY += rows
	}
	return dst
}
