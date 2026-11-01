// Package bench measures the benchmark fixtures under a set of runners and
// reports the medians as a markdown table. Unavailable runners are reported as
// skipped, never as failures. The command is cmd/bench.
package bench

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/siyul-park/spry/benchmarks"
	"github.com/siyul-park/spry/benchmarks/runner"
)

type config struct {
	runners  []string
	fixtures []string
	count    int
	warmup   int
	output   string
}

// Run parses args (the flags of cmd/bench), measures the selected fixtures under
// the selected runners among all, and writes the table to stdout and, with -o,
// into the generated-results section of the named markdown file.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer, all []runner.Runner) error {
	cfg, err := parseFlags(args, stderr)
	if err != nil {
		return err
	}
	runners, err := selectRunners(all, cfg.runners)
	if err != nil {
		return err
	}
	every, err := benchmarks.Fixtures()
	if err != nil {
		return err
	}
	fixtures, err := benchmarks.Select(every, cfg.fixtures)
	if err != nil {
		return err
	}
	return measure(ctx, cfg, runners, fixtures, stdout, stderr)
}

func parseFlags(args []string, stderr io.Writer) (config, error) {
	fs := flag.NewFlagSet("bench", flag.ContinueOnError)
	fs.SetOutput(stderr)
	runners := fs.String("runner", "", "comma-separated runners (default: all)")
	fixtures := fs.String("fixture", "", "comma-separated fixtures or fixture prefixes (default: all)")
	count := fs.Int("count", 3, "measured runs per fixture")
	warmup := fs.Int("warmup", 1, "unmeasured runs per fixture")
	output := fs.String("o", "", "markdown file to update between the generated-results markers")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if *count < 1 || *warmup < 0 {
		return config{}, errors.New("-count must be at least 1 and -warmup at least 0")
	}
	return config{split(*runners), split(*fixtures), *count, *warmup, *output}, nil
}

func split(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

func selectRunners(all []runner.Runner, names []string) ([]runner.Runner, error) {
	if len(names) == 0 {
		return all, nil
	}
	var out []runner.Runner
	seen := map[string]bool{}
	for _, n := range names {
		if seen[n] {
			continue
		}
		seen[n] = true
		found := false
		for _, r := range all {
			if r.Name() == n {
				out = append(out, r)
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("unknown runner %q", n)
		}
	}
	return out, nil
}

func measure(ctx context.Context, cfg config, runners []runner.Runner, fixtures []benchmarks.Fixture, stdout, stderr io.Writer) error {
	results := map[string]map[string]cell{}
	var runnerNames, fixtureNames []string
	for _, r := range runners {
		runnerNames = append(runnerNames, r.Name())
	}
	for _, f := range fixtures {
		fixtureNames = append(fixtureNames, f.Name)
		results[f.Name] = map[string]cell{}
		for _, r := range runners {
			results[f.Name][r.Name()] = measureOne(ctx, cfg, r, f)
		}
	}
	body := table(runnerNames, fixtureNames, results)
	fmt.Fprint(stdout, body)
	if cfg.output == "" {
		return nil
	}
	msg, err := writeReport(cfg.output, body)
	if msg != "" {
		fmt.Fprintln(stderr, msg)
	}
	return err
}

func measureOne(ctx context.Context, cfg config, r runner.Runner, f benchmarks.Fixture) cell {
	if err := r.Available(); err != nil {
		return cell{Skipped: err.Error()}
	}
	var c cell
	for i := 0; i < cfg.warmup+cfg.count; i++ {
		res, err := r.Run(ctx, f)
		if err != nil {
			return cell{Failed: err.Error()}
		}
		if res.Output != f.Expected {
			return cell{Failed: fmt.Sprintf("output %q, want %q", res.Output, f.Expected)}
		}
		if i >= cfg.warmup {
			c.Samples = append(c.Samples, res.Elapsed)
		}
	}
	return c
}
