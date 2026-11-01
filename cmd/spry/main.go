// Command spry runs Python 3.14 programs through the spry compiler.
package main

import (
	"os"

	"github.com/siyul-park/spry/cli"
)

func main() {
	os.Exit(cli.Main(os.Args, os.Stdin, os.Stdout, os.Stderr, os.Environ()))
}
