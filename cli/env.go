package cli

import (
	"strconv"
	"strings"
)

// pythonEnv reads the PYTHON* variables the way CPython does: a variable counts
// when it is set to a non-empty string ("0" included), and the integer ones
// (PYTHONOPTIMIZE, PYTHONVERBOSE) count as many repetitions of their flag as
// their value, or one when the value is not a number. The caarlos0/env library
// was considered and rejected: it parses booleans with strconv.ParseBool, which
// rejects CPython's "any non-empty string" convention, so it would not remove
// code here.
type pythonEnv map[string]string

// newPythonEnv indexes KEY=value entries.
func newPythonEnv(environ []string) pythonEnv {
	env := pythonEnv{}
	for _, entry := range environ {
		if key, value, ok := strings.Cut(entry, "="); ok {
			env[key] = value
		}
	}
	return env
}

// set reports whether name is set to a non-empty string.
func (e pythonEnv) set(name string) bool {
	return e[name] != ""
}

// level returns how many times the matching flag is implied.
func (e pythonEnv) level(name string) int {
	value := e[name]
	if value == "" {
		return 0
	}
	if n, err := strconv.Atoi(value); err == nil {
		return max(n, 0)
	}
	return 1
}

// mergeEnv applies the environment below the flags: booleans and counts only
// ever raise what the flags set, and PYTHONWARNINGS entries come before -W.
func mergeEnv(opts *Options, environ []string) {
	env := newPythonEnv(environ)
	opts.Debug = opts.Debug || env.set("PYTHONDEBUG")
	opts.DontWriteByte = opts.DontWriteByte || env.set("PYTHONDONTWRITEBYTECODE")
	opts.Inspect = opts.Inspect || env.set("PYTHONINSPECT")
	opts.NoUserSite = opts.NoUserSite || env.set("PYTHONNOUSERSITE")
	opts.SafePath = opts.SafePath || env.set("PYTHONSAFEPATH")
	opts.Unbuffered = opts.Unbuffered || env.set("PYTHONUNBUFFERED")
	opts.Optimize = max(opts.Optimize, env.level("PYTHONOPTIMIZE"))
	opts.Verbose = max(opts.Verbose, env.level("PYTHONVERBOSE"))
	if warnings := env["PYTHONWARNINGS"]; warnings != "" {
		opts.Warnings = append(strings.Split(warnings, ","), opts.Warnings...)
	}
}
