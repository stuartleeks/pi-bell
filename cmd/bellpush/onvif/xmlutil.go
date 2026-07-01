package onvif

import (
	"fmt"
	"regexp"
	"strconv"
)

// extractElementText returns the text content of the first element named name
// (any namespace prefix, or none) found in body. Returns "", false if not found.
func extractElementText(body []byte, name string) (string, bool) {
	re := regexp.MustCompile(fmt.Sprintf(`(?s)<(?:[\w-]+:)?%s(?:\s[^>]*)?>([^<]*)</(?:[\w-]+:)?%s>`, name, name))
	m := re.FindSubmatch(body)
	if m == nil {
		return "", false
	}
	return string(m[1]), true
}

// extractIntElement returns the integer value of the first element named name,
// or the provided default if not present or not parseable.
func extractIntElement(body []byte, name string, def int) int {
	text, ok := extractElementText(body, name)
	if !ok {
		return def
	}
	n, err := strconv.Atoi(text)
	if err != nil {
		return def
	}
	return n
}
