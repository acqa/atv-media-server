package library

import (
	"regexp"
	"strconv"
	"strings"
)

// yearRe captures a 4-digit year (1900-2099) optionally wrapped in () or [].
// Anchored to find the LEFTMOST year — typically the release year, not "1080p".
var yearRe = regexp.MustCompile(`[\s._\-]*[\(\[]?((?:19|20)\d{2})[\)\]]?`)

// ParseMovieName extracts (title, year) from a folder or file name.
// Accepts forms like:
//
//	"The Matrix (1999)"     -> "The Matrix", 1999
//	"The.Matrix.1999.1080p" -> "The Matrix", 1999
//	"Inception [2010]"      -> "Inception", 2010
//	"Inception"             -> "Inception", 0
func ParseMovieName(name string) (string, int) {
	if name == "" {
		return "", 0
	}
	if loc := yearRe.FindStringSubmatchIndex(name); loc != nil {
		yearStr := name[loc[2]:loc[3]]
		year, _ := strconv.Atoi(yearStr)
		titleRaw := name[:loc[0]]
		return cleanTitle(titleRaw), year
	}
	return cleanTitle(name), 0
}

func cleanTitle(s string) string {
	s = strings.ReplaceAll(s, ".", " ")
	s = strings.ReplaceAll(s, "_", " ")
	// Collapse runs of whitespace.
	s = strings.Join(strings.Fields(s), " ")
	return strings.TrimSpace(s)
}
