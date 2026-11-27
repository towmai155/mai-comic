package main

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

const (
	dumanwuOrigin    = "https://www.dumanwu.org/"
	dumanwuImageHost = "dmw.546457.xyz"
	// This format value is included in the public browser reader.
	dumanwuReaderKey = "9S8$vJnU2ANeSRoF"
)

var (
	dumanwuChapterPath = regexp.MustCompile(`^/comic/[A-Za-z0-9_-]+\.html$`)
	dumanwuParams      = regexp.MustCompile(`\bvar\s+params\s*=\s*'((?:\\.|[^'\\])*)'\s*;`)
)

func parseDumanwuPages(ctx context.Context, client *http.Client, chapter *url.URL) ([]item, error) {
	if chapter.Scheme != "https" || !dumanwuHost(chapter) || !dumanwuChapterPath.MatchString(chapter.Path) {
		return nil, errors.New("读漫屋章节地址格式无效")
	}
	body, final, err := fetch(ctx, client, chapter, 8<<20)
	if err != nil {
		return nil, err
	}
	if !dumanwuHost(final) || !dumanwuChapterPath.MatchString(final.Path) {
		return nil, errors.New("读漫屋没有提供公开阅读页")
	}
	return decodeDumanwuPages(body, final)
}

func decodeDumanwuPages(body []byte, final *url.URL) ([]item, error) {
	params := dumanwuParams.FindSubmatch(body)
	var encoded string
	if len(params) == 0 || json.Unmarshal([]byte(`"`+string(params[1])+`"`), &encoded) != nil {
		return nil, errors.New("读漫屋阅读页缺少图片参数")
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(raw) <= aes.BlockSize || len(raw)%aes.BlockSize != 0 {
		return nil, errors.New("读漫屋图片参数格式无效")
	}
	block, err := aes.NewCipher([]byte(dumanwuReaderKey))
	if err != nil {
		return nil, err
	}
	iv, data := raw[:aes.BlockSize], raw[aes.BlockSize:]
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(data, data)
	padding := int(data[len(data)-1])
	if padding < 1 || padding > aes.BlockSize ||
		!bytes.Equal(data[len(data)-padding:], bytes.Repeat([]byte{byte(padding)}, padding)) {
		return nil, errors.New("读漫屋图片参数无法解析")
	}
	var manifest struct {
		Host   string   `json:"host"`
		Images []string `json:"images"`
	}
	if json.Unmarshal(data[:len(data)-padding], &manifest) != nil ||
		!strings.EqualFold(manifest.Host, final.Hostname()) || len(manifest.Images) == 0 {
		return nil, errors.New("读漫屋图片列表不可用")
	}
	pages := make([]item, 0, len(manifest.Images))
	for _, address := range manifest.Images {
		if strings.TrimSpace(address) == "" {
			return nil, errors.New("读漫屋返回了空图片地址")
		}
		image, err := resolveURL(final, address)
		if err != nil {
			return nil, err
		}
		pages = append(pages, item{URL: image.String()})
	}
	return pages, nil
}

func dumanwuHost(target *url.URL) bool {
	return target.Hostname() == "www.dumanwu.org" || target.Hostname() == "dumanwu.org"
}
