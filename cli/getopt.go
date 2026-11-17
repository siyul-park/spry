package cli

import (
	"errors"
	"strings"
	"unicode/utf8"
)

type usageError struct {
	message string
}

// option is one option the grammar recognized.
type option struct {
	name   string // the letter of a short option, the name of a long one
	long   bool
	value  string
	hasArg bool
}

// getopt walks a command line with the grammar of CPython 3.14 Python/getopt.c.
type getopt struct {
	args  []string
	index int
	group string // rest of the combined short options being read
}

// shortOptions lists the short options; a following ':' marks one that takes
// a value.
const shortOptions = "bBc:dEhiIm:OPqsSuvVW:xX:?"

// ErrUsage is the category of command line grammar errors.
var ErrUsage = errors.New("usage error")

var longOptions = map[string]bool{
	"help-all":              false,
	"help-env":              false,
	"help-xoptions":         false,
	"check-hash-based-pycs": true,
}

func (e *usageError) Error() string {
	return e.message
}

func (e *usageError) Unwrap() error {
	return ErrUsage
}

// Terminal reports whether the option ends option parsing: -c and -m take
// every following argument as their own.
func (o option) Terminal() bool {
	return !o.long && (o.name == "c" || o.name == "m")
}

func (o option) String() string {
	if o.long {
		return "--" + o.name
	}
	return "-" + o.name
}

func newGetopt(args []string) *getopt {
	return &getopt{args: args}
}

// Operands returns the arguments that follow the options.
func (g *getopt) Operands() []string {
	return g.args[g.index:]
}

// Next returns the next option. It reports false at the first operand, after
// "--", or at the end of the command line.
func (g *getopt) Next() (option, bool, error) {
	if g.group == "" {
		if g.index >= len(g.args) {
			return option{}, false, nil
		}
		switch arg := g.args[g.index]; {
		case arg == "--":
			g.index++
			return option{}, false, nil
		case arg == "--help":
			g.index++
			return option{name: "help", long: true}, true, nil
		case arg == "--version":
			g.index++
			return option{name: "version", long: true}, true, nil
		case !strings.HasPrefix(arg, "-") || arg == "-":
			return option{}, false, nil
		}
		g.group = g.args[g.index][1:]
		g.index++
	}
	letter, size := utf8.DecodeRuneInString(g.group)
	g.group = g.group[size:]
	if letter == '-' {
		return g.long()
	}
	return g.short(letter)
}

func (g *getopt) long() (option, bool, error) {
	name := g.group
	g.group = ""
	if name == "" {
		return option{}, false, nil
	}
	hasArg, ok := longOptions[name]
	if !ok {
		return option{}, false, &usageError{"Unknown option: " + g.args[g.index-1]}
	}
	opt := option{name: name, long: true, hasArg: hasArg}
	if !hasArg {
		return opt, true, nil
	}
	if g.index >= len(g.args) {
		return option{}, false, &usageError{"Argument expected for the " + g.args[g.index-1] + " options"}
	}
	opt.value = g.args[g.index]
	g.index++
	return opt, true, nil
}

func (g *getopt) short(letter rune) (option, bool, error) {
	if letter == 'J' {
		return option{}, false, &usageError{"-J is reserved for Jython"}
	}
	at := strings.IndexRune(shortOptions, letter)
	if at < 0 || letter == ':' {
		return option{}, false, &usageError{"Unknown option: -" + string(letter)}
	}
	opt := option{name: string(letter)}
	if at+1 >= len(shortOptions) || shortOptions[at+1] != ':' {
		return opt, true, nil
	}
	opt.hasArg = true
	switch {
	case g.group != "":
		opt.value, g.group = g.group, ""
	case g.index < len(g.args):
		opt.value = g.args[g.index]
		g.index++
	default:
		return option{}, false, &usageError{"Argument expected for the -" + opt.name + " option"}
	}
	return opt, true, nil
}
