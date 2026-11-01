// Package cli implements the CPython 3.14 compatible command line of spry:
//
//	spry [option] ... [-c cmd | -m mod | file | -] [arg] ...
//
// It parses options exactly like CPython (combined short options, -c and -m
// terminate option parsing, "-" reads stdin, "--" ends options), prints help
// and version texts, validates the spry-only -X options, and dispatches
// execution. It never calls os.Exit; Run returns the exit status.
//
// Input: arguments, standard streams and environment. Output: an exit status.
// Allowed spry imports: the root package spry (once execution lands in phase P3) and
// github.com/siyul-park/minivm.
package cli
