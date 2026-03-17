package cmd

import (
	"github.com/dmora/crucible/internal/config"
	"github.com/spf13/cobra"
)

var updateProvidersCmd = &cobra.Command{
	Use:   "update-providers [url-or-path]",
	Short: "Update provider metadata from the model catalog",
	Long:  "Update provider information from the model catalog, a custom URL, or a local file.",
	Example: `  crucible update-providers                                                              # Fetch from GitHub
  crucible update-providers https://raw.githubusercontent.com/dmora/crucible/main/models.json # Fetch from custom URL
  crucible update-providers /path/to/models.json                                        # Load from local file
  crucible update-providers embedded                                                    # Reset to embedded snapshot`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		var pathOrURL string
		if len(args) > 0 {
			pathOrURL = args[0]
		}
		return config.UpdateProviders(pathOrURL)
	},
}
