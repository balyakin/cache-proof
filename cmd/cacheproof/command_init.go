package main

import (
	"fmt"
	"io"
	"os"

	"cacheproof/internal/appx"
	"cacheproof/internal/config"

	"github.com/spf13/cobra"
)

func newInitCommand(stdout io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create a safe starter cacheproof.yml",
		RunE: func(cmd *cobra.Command, args []string) error {
			const path = "cacheproof.yml"
			if _, err := os.Stat(path); err == nil {
				return fmt.Errorf("%w: %s already exists", appx.ErrConfig, path)
			} else if !os.IsNotExist(err) {
				return fmt.Errorf("%w: stat %s: %v", appx.ErrConfig, path, err)
			}
			if err := os.WriteFile(path, []byte(config.StarterYAML), 0o644); err != nil {
				return fmt.Errorf("%w: write %s: %v", appx.ErrConfig, path, err)
			}
			_, err := fmt.Fprintln(stdout, "created cacheproof.yml")
			return err
		},
	}
}
