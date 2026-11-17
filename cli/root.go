package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Root composes command units and runs one command line against them.
//
// Root never calls cobra's Execute: Execute always registers the hidden
// "__complete" command, which would capture a script of that name. It uses
// cobra for composition (AddCommand, IO and context inherited from the parent,
// Args validators, RunE) and pflag for flag registration and typed values, while
// getopt does the parsing. A Root runs one command line.
type Root struct {
	cmd    *cobra.Command
	units  []*cobra.Command // composition order is selection priority
	flags  *pflag.FlagSet
	owners map[*pflag.Flag]*cobra.Command
}

type exitError struct {
	code int
	err  error
}

var (
	// ErrUnsupported marks an option the grammar knows and no unit implements yet.
	ErrUnsupported = errors.New("is not supported yet")

	// ErrNoCommand means no composed unit selects the command line.
	ErrNoCommand = errors.New("no command selected")
)

// Default is the supported python3.14 command line, one unit per action.
func Default() []*cobra.Command {
	return []*cobra.Command{Help(), Version(), Command(), File(), Stdin()}
}

// New composes units, in selection priority order, under one root.
func New(units ...*cobra.Command) *Root {
	r := &Root{
		cmd:    &cobra.Command{Use: programName},
		units:  units,
		flags:  pflag.NewFlagSet(programName, pflag.ContinueOnError),
		owners: map[*pflag.Flag]*cobra.Command{},
	}
	for _, unit := range units {
		r.cmd.AddCommand(unit)
		unit.Flags().VisitAll(func(flag *pflag.Flag) {
			r.flags.AddFlag(flag)
			r.owners[flag] = unit
		})
	}
	return r
}

// Run executes the command line of p and returns the exit status. Grammar
// errors print the usage reminder; other errors print "spry: <error>".
func (r *Root) Run(ctx context.Context, p Process) int {
	r.cmd.SetIn(p.Stdin)
	r.cmd.SetOut(p.Stdout)
	r.cmd.SetErr(p.Stderr)
	return r.report(r.run(context.WithValue(ctx, processKey{}, p), p.Args))
}

// run reads options in order. The first unsupported option is held back so
// that a grammar error later on the command line, or an immediate unit such as
// help, still wins.
func (r *Root) run(ctx context.Context, args []string) error {
	g := newGetopt(args)
	selected := map[*cobra.Command]bool{}
	var unsupported *option
	for {
		opt, ok, err := g.Next()
		if err != nil {
			return err
		}
		if !ok {
			break
		}
		flag := r.lookup(opt)
		switch {
		case flag == nil && unsupported == nil:
			unsupported = &opt
		case flag != nil:
			unit := r.owners[flag]
			if err := record(flag, opt); err != nil {
				return err
			}
			selected[unit] = true
			if unit.Annotations[annotationImmediate] != "" {
				return r.exec(ctx, unit, g.Operands())
			}
		}
		if opt.Terminal() {
			break
		}
	}
	if unsupported != nil {
		return &exitError{code: ExitUsage, err: fmt.Errorf("option %s %w", unsupported, ErrUnsupported)}
	}
	unit := r.pick(selected, g.Operands())
	if unit == nil {
		return ErrNoCommand
	}
	return r.exec(ctx, unit, g.Operands())
}

// pick chooses what to run: the first unit whose flag was met, else the first
// flagless unit whose Args validator accepts the operands.
func (r *Root) pick(selected map[*cobra.Command]bool, operands []string) *cobra.Command {
	for _, unit := range r.units {
		if selected[unit] && unit.RunE != nil {
			return unit
		}
	}
	for _, unit := range r.units {
		if !unit.Flags().HasFlags() && unit.RunE != nil && unit.ValidateArgs(operands) == nil {
			return unit
		}
	}
	return nil
}

func (r *Root) lookup(opt option) *pflag.Flag {
	if opt.long {
		return r.flags.Lookup(opt.name)
	}
	return r.flags.ShorthandLookup(opt.name)
}

func (r *Root) exec(ctx context.Context, unit *cobra.Command, args []string) error {
	unit.SetContext(ctx)
	return unit.RunE(unit, args)
}

func (r *Root) report(err error) int {
	if err == nil {
		return ExitOK
	}
	stderr := r.cmd.ErrOrStderr()
	if errors.Is(err, ErrUsage) {
		_, _ = fmt.Fprintln(stderr, err)
		_, _ = io.WriteString(stderr, usageLine+usageHint)
		return ExitUsage
	}
	_, _ = fmt.Fprintf(stderr, "%s: %v\n", programName, err)
	var exit *exitError
	if errors.As(err, &exit) {
		return exit.code
	}
	return ExitFailure
}

func (e *exitError) Error() string {
	return e.err.Error()
}

func (e *exitError) Unwrap() error {
	return e.err
}

// record stores the option in the flag it selected, as pflag's parser would.
func record(flag *pflag.Flag, opt option) error {
	value := flag.NoOptDefVal
	if opt.hasArg {
		value = opt.value
	}
	if err := flag.Value.Set(value); err != nil {
		return err
	}
	flag.Changed = true
	return nil
}
