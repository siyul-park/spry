// Package cli is the command layer of spry: the CPython 3.14 command line
//
//	spry [option] ... [-c cmd | -m mod | file | -] [arg] ...
//
// composed from cobra command units. Each action the command line can select
// (help, version, -c, file, stdin) is one *cobra.Command that owns its pflag
// flags; New composes them under a Root, whose order is the selection
// priority. Options are read by a port of CPython's getopt (the whole 3.14
// grammar), not by pflag's parser. An option in the grammar that no unit
// registers is reported as not supported yet.
//
// cli maps argv to stage calls and stage results to CPython-format output and
// exit statuses; language semantics belong to the stage packages. It never
// calls os.Exit; Run returns the exit status.
//
// Input: arguments, standard streams, environment and working directory.
// Output: an exit status.
// Allowed spry imports: the stage packages and github.com/siyul-park/minivm,
// plus github.com/spf13/cobra and github.com/spf13/pflag.
package cli
