package livetv

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// CategoryNameLess reports whether category name a should sort before b.
// Letter-leading names come first (A…Z), then numbers/symbols/other.
func CategoryNameLess(a, b string) bool {
	ka, ba := categorySortKey(a)
	kb, bb := categorySortKey(b)
	if ba != bb {
		return ba < bb
	}
	if ka != kb {
		return ka < kb
	}
	return a < b
}

func categorySortKey(name string) (key string, bucket int) {
	name = strings.TrimSpace(name)
	key = strings.ToLower(name)
	if name == "" {
		return key, 2
	}
	r, _ := utf8.DecodeRuneInString(strings.TrimLeftFunc(name, unicode.IsSpace))
	if r == utf8.RuneError {
		return key, 2
	}
	if unicode.IsLetter(r) {
		return key, 0
	}
	return key, 1
}

// SortXtreamCategories sorts panel categories: letters first, then the rest.
func SortXtreamCategories(cats []XtreamCategory) {
	sort.SliceStable(cats, func(i, j int) bool {
		return CategoryNameLess(cats[i].CategoryName, cats[j].CategoryName)
	})
}

// SortNamed sorts any string names with the same letter-first rule.
func SortNamed(names []string) {
	sort.SliceStable(names, func(i, j int) bool {
		return CategoryNameLess(names[i], names[j])
	})
}
