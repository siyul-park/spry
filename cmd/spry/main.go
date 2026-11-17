// Command spry runs Python 3.14 programs through the spry compiler.
package main

import (
	"context"
	"os"

	"github.com/siyul-park/spry/cli"
)

func main() {
	os.Exit(cli.Run(context.Background(), cli.Process{
		Args:   os.Args[1:],
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Env:    os.Environ(),
	}))
}
