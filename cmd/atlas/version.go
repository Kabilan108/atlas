package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version can be overridden at build time using -ldflags.
var Version = "0.1.0-dev"

func newVersionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the atlas CLI version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintln(cmd.OutOrStdout(), Version)
		},
	}
	cmd.Annotations = map[string]string{"skipInit": "true"}
	return cmd
}
