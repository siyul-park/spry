package cli

import (
	"strings"

	"github.com/spf13/pflag"
)

// split is a command line cut where CPython stops reading options.
type split struct {
	// options are the arguments for the flag parser: everything before the
	// first terminal flag (-c or -m), or the whole command line when there is
	// none.
	options []string
	// terminal is set when a terminal flag cut the command line.
	terminal *Terminal
	// err is a command line error found after options; it is reported only
	// when options themselves parse without an earlier error or preempting
	// request.
	err *UsageError
}

// splitArgs finds where CPython stops option parsing. The flag library cannot
// express that -c and -m swallow every later argument, even from inside a
// combined group such as -Ec, so the command line is cut here and only the
// prefix is handed to it. terminals maps the letters of terminal flags to their
// modes. The scan stops at "--" or the first positional argument and skips the
// values of flags that take one.
//
// Long options are only the flags registered with a name longer than one
// letter; everything else starting with "--", including "--name=value", is an
// unknown option, as in CPython.
//
// It also keeps a "=" that follows a valued short flag as part of the value, as
// CPython does, by passing the value as a separate argument ("-W=x" becomes
// "-W", "=x"); pflag would drop the "=".
func splitArgs(args []string, fs *pflag.FlagSet, terminals map[byte]Mode) split {
	var options []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--" || arg == "-" || !strings.HasPrefix(arg, "-"):
			return split{options: append(options, args[i:]...)}
		case strings.HasPrefix(arg, "--"):
			flag := fs.Lookup(arg[2:])
			if flag == nil || len(flag.Name) < 2 {
				return split{options: options, err: &UsageError{Message: "Unknown option: " + arg}}
			}
			options = append(options, arg)
			if takesValue(flag) && i+1 < len(args) {
				i++
				options = append(options, args[i])
			}
		default:
			group, consumed, cut := scanGroup(args, i, fs, terminals)
			if cut != nil {
				cut.options = append(options, cut.options...)
				return *cut
			}
			options = append(options, group...)
			i += consumed
		}
	}
	return split{options: options}
}

// scanGroup scans one "-abc" group. When the group holds a terminal flag it
// returns the cut command line. Otherwise it returns the argument(s) to pass on
// and how many further arguments the group consumed (the separate value of a
// valued flag).
func scanGroup(args []string, i int, fs *pflag.FlagSet, terminals map[byte]Mode) (group []string, consumed int, cut *split) {
	arg := args[i]
	for j := 1; j < len(arg); j++ {
		if mode, ok := terminals[arg[j]]; ok {
			cutArgs := cutAt(args, i, j, mode)
			return nil, 0, &cutArgs
		}
		flag := fs.ShorthandLookup(arg[j : j+1])
		if flag == nil || !takesValue(flag) {
			continue
		}
		switch {
		case j+1 == len(arg) && i+1 < len(args):
			return args[i : i+2], 1, nil
		case j+1 < len(arg) && arg[j+1] == '=':
			return []string{arg[:j+1], arg[j+1:]}, 0, nil
		}
		return args[i : i+1], 0, nil
	}
	return args[i : i+1], 0, nil
}

// cutAt cuts args at flag group args[i], whose letter at index j is a terminal
// flag selecting mode. Its options hold only the letters of the group before
// that flag.
func cutAt(args []string, i, j int, mode Mode) split {
	group := args[i]
	var options []string
	if j > 1 {
		options = append(options, group[:j])
	}
	value, rest := group[j+1:], args[i+1:]
	if value == "" {
		if len(rest) == 0 {
			return split{options: options, err: &UsageError{Message: "Argument expected for the -" + group[j:j+1] + " option"}}
		}
		value, rest = rest[0], rest[1:]
	}
	return split{options: options, terminal: &Terminal{Mode: mode, Value: value, Rest: rest}}
}

// takesValue reports whether the flag needs a separate or attached value.
func takesValue(flag *pflag.Flag) bool {
	return flag.NoOptDefVal == ""
}
