package cli

import (
	"fmt"
	"io"
	"runtime"

	"github.com/spf13/cobra"
)

// Version is -V and --version. Given twice (-VV) it prints build details.
func Version() *cobra.Command {
	cmd := &cobra.Command{
		Use:  "version",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			count, err := cmd.Flags().GetCount("version")
			if err != nil {
				return err
			}
			text := fmt.Sprintf("Python %s (%s %s)", pythonVersion, programName, Release)
			if count >= 2 {
				text += fmt.Sprintf(" [Go %s %s/%s]", runtime.Version(), runtime.GOOS, runtime.GOARCH)
			}
			_, err = io.WriteString(cmd.OutOrStdout(), text+"\n")
			return err
		},
	}
	cmd.Flags().CountP("version", "V", "print the Python version number and exit")
	return cmd
}
