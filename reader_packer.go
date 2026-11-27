package main

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

var (
	packedScript = regexp.MustCompile(`\}\(\s*'((?:\\.|[^'\\])*)'\s*,\s*([0-9]+)\s*,\s*[0-9]+\s*,\s*'([^']*)'\.split\('\|'\)`)
	packedWord   = regexp.MustCompile(`\b[0-9A-Za-z]+\b`)
)

// Expand the reader's packed string table without running its script.
func unpackReaderScript(body string) (string, error) {
	packed := packedScript.FindStringSubmatch(body)
	if len(packed) == 0 {
		return body, nil
	}
	radix, err := strconv.Atoi(packed[2])
	if err != nil || radix < 2 || radix > 62 {
		return "", errors.New("无法识别阅读页数据编码")
	}
	dictionary := strings.Split(packed[3], "|")
	const alphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	decoded := packedWord.ReplaceAllStringFunc(packed[1], func(word string) string {
		index := 0
		for i := range len(word) {
			digit := strings.IndexByte(alphabet, word[i])
			if digit < 0 || digit >= radix || index >= len(dictionary) {
				return word
			}
			index = index*radix + digit
		}
		if index >= len(dictionary) || dictionary[index] == "" {
			return word
		}
		return dictionary[index]
	})
	return strings.NewReplacer(`\'`, "'", `\\`, `\`).Replace(decoded), nil
}
