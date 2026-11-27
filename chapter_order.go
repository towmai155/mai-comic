package main

import (
	"regexp"
	"slices"
	"strconv"
)

var chapterOrdinal = regexp.MustCompile(`(?i)^\s*(?:(?:第|chapter|ch\.?|episode|ep\.?)\s*)?([0-9]+(?:\.[0-9]+)?|[零〇一二两兩三四五六七八九十百千]+)(?:\s|[话話章节節回集卷:：.、-]|$)`)

func (a *app) chapterNeighbor(current, offset int) int {
	if a.sourceNewestFirst {
		offset = -offset
	}
	index := current + offset
	if index < 0 || index >= len(a.chapters) {
		return -1
	}
	return index
}

// Keep the source's sequence of extras and volumes. Compare numbered entries
// only to tell which end of that sequence is the beginning.
func newestChapterFirst(chapters []item) bool {
	var previous float64
	seen, direction := false, 0
	for _, chapter := range chapters {
		match := chapterOrdinal.FindStringSubmatch(chapter.Title)
		if match == nil {
			continue
		}
		number := parseChapterOrdinal(match[1])
		if seen {
			switch {
			case number < previous:
				direction--
			case number > previous:
				direction++
			}
		}
		previous, seen = number, true
	}
	return direction < 0
}

func parseChapterOrdinal(value string) float64 {
	if number, err := strconv.ParseFloat(value, 64); err == nil {
		return number
	}
	total, digits := 0, 0
	for _, r := range value {
		var unit int
		switch r {
		case '十':
			unit = 10
		case '百':
			unit = 100
		case '千':
			unit = 1000
		case '〇':
			r = '零'
		case '两', '兩':
			r = '二'
		}
		if unit > 0 {
			total += max(1, digits) * unit
			digits = 0
		} else if digit := slices.Index([]rune("零一二三四五六七八九"), r); digit >= 0 {
			digits = 10*digits + digit
		}
	}
	return float64(total + digits)
}
