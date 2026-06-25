package main

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

func newVersionCommand(stdout io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print cacheproof version",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintln(stdout, version)
			return err
		},
	}
}
