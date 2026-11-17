package token_test

import (
	"encoding/json"
	"os"
	"sort"
	"strconv"
	"testing"

	"github.com/siyul-park/spry/token"
	"github.com/stretchr/testify/require"
)

type tables struct {
	ExactTokenTypes map[string]string `json:"exact_token_types"`
	Kwlist          []string          `json:"kwlist"`
	Softkwlist      []string          `json:"softkwlist"`
}

func TestToken(t *testing.T) {
	tab := loadTables(t)
	spellings := map[string]int{}
	for tok := token.ILLEGAL; tok <= token.YIELD; tok++ {
		if tok.IsOperator() {
			spellings[tok.String()]++
		}
	}
	require.Len(t, spellings, len(tab.ExactTokenTypes))
	for spelling, n := range spellings {
		require.Contains(t, tab.ExactTokenTypes, spelling)
		require.Equal(t, 1, n, spelling)
	}
}

func TestLookup(t *testing.T) {
	tab := loadTables(t)
	for _, kw := range tab.Kwlist {
		require.True(t, token.Lookup(kw).IsKeyword(), kw)
		require.Equal(t, kw, token.Lookup(kw).String())
	}

	tests := []struct {
		ident string
		want  token.Token
	}{
		{"def", token.DEF},
		{"True", token.TRUE},
		{"true", token.NAME},
		{"match", token.NAME},
		{"_", token.NAME},
		{"spam", token.NAME},
		{"", token.NAME},
	}
	for _, tt := range tests {
		t.Run(tt.ident, func(t *testing.T) {
			require.Equal(t, tt.want, token.Lookup(tt.ident))
		})
	}
}

func TestIsKeyword(t *testing.T) {
	tab := loadTables(t)
	var got []string
	for tok := token.ILLEGAL; tok <= token.YIELD; tok++ {
		if tok.IsKeyword() {
			got = append(got, tok.String())
		}
	}
	sort.Strings(got)
	want := append([]string(nil), tab.Kwlist...)
	sort.Strings(want)
	require.Equal(t, want, got)

	tests := []struct {
		name string
		want bool
	}{
		{"None", true},
		{"async", true},
		{"none", false},
		{"match", false},
		{"print", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, token.IsKeyword(tt.name))
		})
	}
}

func TestIsSoftKeyword(t *testing.T) {
	tab := loadTables(t)
	for _, kw := range tab.Softkwlist {
		require.True(t, token.IsSoftKeyword(kw), kw)
	}
	for _, name := range []string{"def", "Match", "matches", "", "print"} {
		require.False(t, token.IsSoftKeyword(name), name)
	}
}

func TestIsIdentifier(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"spam", true},
		{"_private", true},
		{"__dunder__", true},
		{"x1", true},
		{"match", true},
		{"_", true},
		{"变量", true},
		{"naïve", true},
		{"ⅰ", true},
		{"á", true},
		{"", false},
		{"1x", false},
		{"a-b", false},
		{"a b", false},
		{"́a", false},
		{"def", false},
		{"None", false},
		{"$x", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, token.IsIdentifier(tt.name))
		})
	}
}

func TestToken_Precedence(t *testing.T) {
	tests := []struct {
		tok  token.Token
		want int
	}{
		{token.BIT_OR, 1},
		{token.BIT_XOR, 2},
		{token.BIT_AND, 3},
		{token.SHL, 4},
		{token.SHR, 4},
		{token.ADD, 5},
		{token.SUB, 5},
		{token.MUL, 6},
		{token.AT, 6},
		{token.QUO, 6},
		{token.FLOOR_QUO, 6},
		{token.REM, 6},
		{token.POW, token.LowestPrec},
		{token.EQL, token.LowestPrec},
		{token.NOT, token.LowestPrec},
		{token.AND, token.LowestPrec},
		{token.NAME, token.LowestPrec},
	}
	for _, tt := range tests {
		t.Run(tt.tok.String(), func(t *testing.T) {
			require.Equal(t, tt.want, tt.tok.Precedence())
		})
	}
	require.Less(t, token.LowestPrec, token.UnaryPrec)
	require.Less(t, token.UnaryPrec, token.HighestPrec)
	require.Less(t, token.MUL.Precedence(), token.UnaryPrec)
}

func TestToken_IsLiteral(t *testing.T) {
	tests := []struct {
		tok  token.Token
		want bool
	}{
		{token.NAME, true},
		{token.NUMBER, true},
		{token.STRING, true},
		{token.FSTRING_START, true},
		{token.TSTRING_END, true},
		{token.NEWLINE, false},
		{token.ADD, false},
		{token.DEF, false},
		{token.ILLEGAL, false},
	}
	for _, tt := range tests {
		t.Run(tt.tok.String(), func(t *testing.T) {
			require.Equal(t, tt.want, tt.tok.IsLiteral())
		})
	}
}

func TestToken_IsOperator(t *testing.T) {
	tests := []struct {
		tok  token.Token
		want bool
	}{
		{token.ADD, true},
		{token.ARROW, true},
		{token.LPAREN, true},
		{token.EXCLAMATION, true},
		{token.NAME, false},
		{token.INDENT, false},
		{token.DEF, false},
		{token.EOF, false},
	}
	for _, tt := range tests {
		t.Run(tt.tok.String(), func(t *testing.T) {
			require.Equal(t, tt.want, tt.tok.IsOperator())
		})
	}
}

func TestToken_IsKeyword(t *testing.T) {
	tests := []struct {
		tok  token.Token
		want bool
	}{
		{token.FALSE, true},
		{token.YIELD, true},
		{token.NOT, true},
		{token.NAME, false},
		{token.ADD, false},
		{token.DEDENT, false},
		{token.Token(1000), false},
	}
	for _, tt := range tests {
		t.Run(tt.tok.String(), func(t *testing.T) {
			require.Equal(t, tt.want, tt.tok.IsKeyword())
		})
	}
}

func TestToken_String(t *testing.T) {
	tests := []struct {
		name string
		tok  token.Token
		want string
	}{
		{"special", token.EOF, "EOF"},
		{"literal", token.NAME, "NAME"},
		{"layout", token.DEDENT, "DEDENT"},
		{"operator", token.FLOOR_QUO_ASSIGN, "//="},
		{"exclamation", token.EXCLAMATION, "!"},
		{"keyword", token.DEF, "def"},
		{"capitalized keyword", token.FALSE, "False"},
		{"unknown", token.Token(1000), "token(1000)"},
		{"negative", token.Token(-1), "token(-1)"},
		{"range marker", token.Token(token.YIELD + 1), "token(" + strconv.Itoa(int(token.YIELD)+1) + ")"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.tok.String())
		})
	}
}

func loadTables(t *testing.T) tables {
	t.Helper()
	data, err := os.ReadFile("testdata/cpython/tables.json")
	require.NoError(t, err)
	var tab tables
	require.NoError(t, json.Unmarshal(data, &tab))
	return tab
}
