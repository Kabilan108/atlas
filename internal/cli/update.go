package cli

import (
	"fmt"

	updatepkg "github.com/kabilan108/atlas/internal/update"
	"github.com/spf13/cobra"
)

func newUpdateCmd(version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "update",
		Short:        "Update a standalone Atlas binary",
		Long:         "Update Atlas when it was installed as a standalone release binary. Package-manager and immutable installs are not supported.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpdate(cmd, version)
		},
	}
	return cmd
}

func runUpdate(cmd *cobra.Command, version string) error {
	result, err := (updatepkg.Updater{}).Update(version, "")
	if err != nil {
		return fmt.Errorf("%w\n\nInstall the standalone binary with:\n  curl -fsSL https://raw.githubusercontent.com/kabilan108/atlas/master/install.sh | sh", err)
	}
	if !result.Updated {
		fmt.Fprintf(cmd.OutOrStdout(), "Atlas is already up to date at %s\n", result.CurrentVersion)
		return nil
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Updated atlas %s -> %s\n", result.CurrentVersion, result.LatestVersion)
	return nil
}
