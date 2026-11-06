// SPDX-License-Identifier: AGPL-3.0-or-later

// Package i18n translates texts for display (Q-07). English is the source language: the code contains
// English texts, de.json maps them to German. {} in a catalog key stands for a value that is filled in
// at runtime. Translation happens where text is shown (assistant, console); the web interface fetches
// the same catalog and does the same in web/static/i18n.js. Unknown texts stay as they are, so a missing
// translation only ever shows English.
package i18n

import (
	_ "embed"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"sync"
)

//go:embed de.json
var deJSON []byte

type pattern struct {
	re  *regexp.Regexp
	val string
}

type catalog struct {
	exact    map[string]string
	patterns []pattern
}

var (
	mu      sync.RWMutex
	current *catalog
	loadDE  = sync.OnceValue(func() *catalog { return parse(deJSON) })
)

// parse builds a catalog. Longer keys come first, so the most specific pattern wins.
func parse(data []byte) *catalog {
	raw := map[string]string{}
	if err := json.Unmarshal(data, &raw); err != nil {
		panic("i18n: " + err.Error())
	}
	c := &catalog{exact: map[string]string{}}
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(a, b int) bool {
		if len(keys[a]) != len(keys[b]) {
			return len(keys[a]) > len(keys[b])
		}
		return keys[a] < keys[b]
	})
	for _, k := range keys {
		if !strings.Contains(k, "{}") {
			c.exact[k] = raw[k]
			if t := strings.TrimSpace(k); t != k {
				if _, ok := raw[t]; !ok {
					c.exact[t] = strings.TrimSpace(raw[k])
				}
			}
			continue
		}
		// Patterns are matched against trimmed text, the caller keeps the surrounding whitespace.
		parts := strings.Split(strings.TrimSpace(k), "{}")
		for i, p := range parts {
			parts[i] = regexp.QuoteMeta(p)
		}
		// Only a pattern that spans lines itself may match across lines. The others would swallow
		// a whole text whose first line happens to match.
		flags := ""
		if strings.Contains(strings.TrimSpace(k), "\n") {
			flags = "(?s)"
		}
		c.patterns = append(c.patterns, pattern{regexp.MustCompile(flags + `^` + strings.Join(parts, `(.*?)`) + `$`), strings.TrimSpace(raw[k])})
	}
	return c
}

// Lang returns the language of the interface for a system locale: "de" or "en".
func Lang(locale string) string {
	if strings.HasPrefix(locale, "de") {
		return "de"
	}
	return "en"
}

// Catalog returns the catalog of a language as JSON for the web interface, nil for English.
func Catalog(lang string) []byte {
	if lang == "de" {
		return deJSON
	}
	return nil
}

// SetLocale chooses the language for T from a system locale such as de_AT.UTF-8.
func SetLocale(locale string) {
	mu.Lock()
	defer mu.Unlock()
	if Lang(locale) == "de" {
		current = loadDE()
	} else {
		current = nil
	}
}

// T translates a text into the chosen language.
func T(s string) string {
	mu.RLock()
	c := current
	mu.RUnlock()
	if c == nil {
		return s
	}
	return c.translate(s, 0)
}

var fieldPrefix = regexp.MustCompile(`^[a-z0-9_.\[\]-]+: `)

func (c *catalog) translate(s string, depth int) string {
	if v, ok := c.exact[s]; ok {
		return v
	}
	trimmed := strings.TrimSpace(s)
	if trimmed == "" || depth > 5 {
		return s
	}
	start := strings.Index(s, trimmed)
	lead, tail := s[:start], s[start+len(trimmed):]
	if v, ok := c.lookup(trimmed, depth); ok {
		return lead + v + tail
	}
	// Several lines or a list of values: each part on its own.
	for _, sep := range []string{"\n", ", "} {
		if strings.Contains(trimmed, sep) {
			parts := strings.Split(trimmed, sep)
			for i, p := range parts {
				parts[i] = c.translate(p, depth+1)
			}
			return lead + strings.Join(parts, sep) + tail
		}
	}
	// Errors of a configuration field: "lan.ipv4: message"
	if p := fieldPrefix.FindString(trimmed); p != "" {
		return lead + p + c.translate(trimmed[len(p):], depth+1) + tail
	}
	return s
}

func (c *catalog) lookup(s string, depth int) (string, bool) {
	if v, ok := c.exact[s]; ok {
		return v, true
	}
	for _, p := range c.patterns {
		m := p.re.FindStringSubmatch(s)
		if m == nil {
			continue
		}
		out := p.val
		for _, val := range m[1:] {
			out = strings.Replace(out, "{}", c.translate(val, depth+1), 1)
		}
		return out, true
	}
	return "", false
}
