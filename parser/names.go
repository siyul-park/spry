package parser

import (
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"strconv"
	"strings"
	"sync"
)

//go:generate go run gen_names.go

// nameRange is a run of characters named prefix followed by the code point in
// upper-case hex, such as "CJK UNIFIED IDEOGRAPH-4E00".
type nameRange struct {
	lo, hi rune
	prefix string
}

//go:embed names.txt.gz
var namesData []byte

var (
	namesOnce  sync.Once
	namesByKey map[string]rune
	nameRanges []nameRange
)

var (
	hangulInitials = []string{"G", "GG", "N", "D", "DD", "R", "M", "B", "BB", "S", "SS", "", "J", "JJ", "C", "K", "T", "P", "H"}
	hangulVowels   = []string{"A", "AE", "YA", "YAE", "EO", "E", "YEO", "YE", "O", "WA", "WAE", "OE", "YO", "U", "WEO", "WE", "WI", "YU", "EU", "YI", "I"}
	hangulFinals   = []string{"", "G", "GG", "GS", "N", "NJ", "NH", "D", "L", "LG", "LM", "LB", "LS", "LT", "LP", "LH", "M", "B", "BS", "S", "SS", "NG", "J", "C", "K", "T", "P", "H"}
)

// unicodeName resolves the name of a \N{...} escape the way CPython does:
// case-insensitively, including aliases, Hangul syllables and the ideograph
// ranges. Named sequences are not accepted.
func unicodeName(name string) (rune, bool) {
	namesOnce.Do(loadNames)
	name = strings.ToUpper(name)
	if r, ok := namesByKey[name]; ok {
		return r, true
	}
	if rest, ok := strings.CutPrefix(name, "HANGUL SYLLABLE "); ok {
		return hangulSyllable(rest)
	}
	for _, nr := range nameRanges {
		if hex, ok := strings.CutPrefix(name, nr.prefix); ok && len(hex) >= 4 && hex == strings.ToUpper(hex) {
			if v, err := strconv.ParseInt(hex, 16, 32); err == nil && rune(v) >= nr.lo && rune(v) <= nr.hi {
				return rune(v), true
			}
		}
	}
	return 0, false
}

func loadNames() {
	namesByKey = make(map[string]rune, 40000)
	zr, err := gzip.NewReader(bytes.NewReader(namesData))
	if err != nil {
		panic("parser: corrupt names.txt.gz: " + err.Error())
	}
	sc := bufio.NewScanner(zr)
	for sc.Scan() {
		fields := strings.SplitN(sc.Text(), " ", 4)
		switch {
		case fields[0] == "R" && len(fields) == 4:
			lo, _ := strconv.ParseInt(fields[1], 16, 32)
			hi, _ := strconv.ParseInt(fields[2], 16, 32)
			nameRanges = append(nameRanges, nameRange{rune(lo), rune(hi), fields[3]})
		case fields[0] == "A" && len(fields) >= 3:
			cp, _ := strconv.ParseInt(fields[1], 16, 32)
			namesByKey[strings.Join(fields[2:], " ")] = rune(cp)
		case len(fields) >= 2:
			cp, _ := strconv.ParseInt(fields[0], 16, 32)
			namesByKey[strings.Join(fields[1:], " ")] = rune(cp)
		}
	}
}

// hangulSyllable decodes the jamo letters of a HANGUL SYLLABLE name.
func hangulSyllable(jamo string) (rune, bool) {
	for l, li := range hangulInitials {
		rest, ok := strings.CutPrefix(jamo, li)
		if !ok {
			continue
		}
		for v, vi := range hangulVowels {
			rest2, ok := strings.CutPrefix(rest, vi)
			if !ok {
				continue
			}
			for t, ti := range hangulFinals {
				if rest2 == ti {
					return rune(0xAC00 + (l*21+v)*28 + t), true
				}
			}
		}
	}
	return 0, false
}
