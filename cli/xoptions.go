package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/pflag"
)

// XOptions is -X. It keeps every value, like CPython, and validates the
// spry-only ones ("spry.dump", "spry.opt", "spry.fallback", "spry.emit").
type XOptions struct{}

// SpryOptions holds the spry-only -X options.
type SpryOptions struct {
	Dump     string // "", tokens, ast, types, ssa or asm
	Opt      int    // OptDefault or 0..3
	Fallback string // "" , warn or error
	Emit     string // path of the minivm program to write
}

// OptDefault means -X spry.opt was not given and the compiler picks the level.
const OptDefault = -1

const (
	maxOptLevel    = 3
	spryOptionPref = "spry."
)

var (
	dumpStages    = []string{"tokens", "ast", "types", "ssa", "asm"}
	fallbackModes = []string{"warn", "error"}
)

func (XOptions) Register(fs *pflag.FlagSet, _ *State) {
	fs.StringArrayP("X", "X", nil, "")
}

func (XOptions) Prepare(st *State) error {
	st.Options.XOptions, _ = st.Flags.GetStringArray("X")
	spry, err := parseSpryOptions(st.Options.XOptions)
	if err != nil {
		return err
	}
	st.Options.Spry = spry
	return nil
}

// parseSpryOptions validates the -X values that start with "spry.". Other -X
// values are accepted untouched, like CPython does for unknown implementation
// options. The last occurrence of an option wins.
func parseSpryOptions(xopts []string) (SpryOptions, error) {
	out := SpryOptions{Opt: OptDefault}
	for _, x := range xopts {
		name, value, hasValue := strings.Cut(x, "=")
		if !strings.HasPrefix(name, spryOptionPref) {
			continue
		}
		switch name {
		case "spry.dump":
			if !hasValue || !contains(dumpStages, value) {
				return out, xOptionError(x, "expected one of "+strings.Join(dumpStages, ", "))
			}
			out.Dump = value
		case "spry.opt":
			level, err := strconv.Atoi(value)
			if !hasValue || err != nil || !isDigits(value) || level < 0 || level > maxOptLevel {
				return out, xOptionError(x, "expected an integer from 0 to 3")
			}
			out.Opt = level
		case "spry.fallback":
			if !hasValue || !contains(fallbackModes, value) {
				return out, xOptionError(x, "expected one of "+strings.Join(fallbackModes, ", "))
			}
			out.Fallback = value
		case "spry.emit":
			if !hasValue || value == "" {
				return out, xOptionError(x, "expected a file path")
			}
			out.Emit = value
		default:
			return out, xOptionError(x, "unknown spry option")
		}
	}
	return out, nil
}

func xOptionError(option, reason string) error {
	return &UsageError{Message: fmt.Sprintf("invalid -X option %q: %s", option, reason), NoUsage: true}
}

func contains(set []string, s string) bool {
	for _, v := range set {
		if v == s {
			return true
		}
	}
	return false
}

func isDigits(s string) bool {
	return s != "" && strings.Trim(s, "0123456789") == ""
}
