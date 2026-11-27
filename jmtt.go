package main

import (
	"net/url"
	"strings"
)

const jmttShareHost = "jm-uu.cc"
const jmttHost = "comic18j-susu.com"
const jmttNewHost = "jmcomic-zzz.one"
const jmttFufiHost = "comic18j-fufi.com"

func isJMTTHost(host string) bool {
	switch strings.ToLower(host) {
	case jmttHost, jmttNewHost, jmttFufiHost:
		return true
	default:
		return false
	}
}

// Accept an explicitly entered JMTT shortcut or homepage without treating
// other HTML responses as source configurations.
func importJMTTWebsite(target, response *url.URL) (source, bool) {
	if response.Scheme != "https" || (response.Path != "" && response.Path != "/") {
		return source{}, false
	}
	if target.Scheme != "https" || target.RawQuery != "" || target.Fragment != "" {
		return source{}, false
	}
	home := jmttReaderHost(target) && (target.Path == "" || target.Path == "/")
	code := strings.TrimPrefix(target.Path, "/")
	shortLink := strings.EqualFold(target.Hostname(), jmttShareHost) && code != "" && !strings.Contains(code, "/")
	if !home && !shortLink {
		return source{}, false
	}
	return adaptCimocSource("JMTT", response.Scheme+"://"+response.Host)
}
