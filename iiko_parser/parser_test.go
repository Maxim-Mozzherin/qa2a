package main

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestNaturalPageSorting(t *testing.T) {
	matches := []string{
		"temp/upd_123_img-1.jpg",
		"temp/upd_123_img-10.jpg",
		"temp/upd_123_img-2.jpg",
		"temp/upd_123_img-3.jpg",
		"temp/upd_123_img-20.jpg",
	}

	sort.Slice(matches, func(i, j int) bool {
		getPageNum := func(path string) int {
			base := filepath.Base(path)
			numPart := strings.TrimSuffix(base, ".jpg")
			idx := strings.LastIndex(numPart, "-")
			if idx != -1 {
				if n, err := strconv.Atoi(numPart[idx+1:]); err == nil {
					return n
				}
			}
			return 0
		}
		return getPageNum(matches[i]) < getPageNum(matches[j])
	})

	expected := []string{
		"temp/upd_123_img-1.jpg",
		"temp/upd_123_img-2.jpg",
		"temp/upd_123_img-3.jpg",
		"temp/upd_123_img-10.jpg",
		"temp/upd_123_img-20.jpg",
	}

	for i, match := range matches {
		if match != expected[i] {
			t.Errorf("at index %d: expected %s, got %s", i, expected[i], match)
		}
	}
}
