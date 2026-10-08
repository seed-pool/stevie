package scanner

import (
	"crypto/sha256"
	"encoding/binary"
	"strconv"
	"strings"
)

// localTMDBID returns a stable negative id for unmatched local titles.
func localTMDBID(parts ...string) int {
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	n := int(binary.BigEndian.Uint32(sum[:4]) & 0x7fffffff)
	if n == 0 {
		n = 1
	}
	return -n
}

func yearDate(year int) string {
	if year <= 0 {
		return ""
	}
	return strconv.Itoa(year) + "-01-01"
}
