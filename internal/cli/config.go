package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/kabilan108/atlas/internal/config"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage atlas configuration",
	}

	cmd.AddCommand(newConfigSetCmd())
	cmd.AddCommand(newConfigGetCmd())
	cmd.AddCommand(newConfigVerifyCmd())

	return cmd
}

func newConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> [value]",
		Short: "Set a configuration value",
		Long: `Set a configuration value. Valid keys: workspace, username, app_password.

For app_password, if no value is provided, you will be prompted to enter it interactively
(hidden input). You can also pipe the value via stdin.

Examples:
  atlas config set workspace mycompany
  atlas config set username user@example.com
  atlas config set app_password              # prompts interactively
  echo $TOKEN | atlas config set app_password  # via stdin`,
		Args: cobra.RangeArgs(1, 2),
		RunE: runConfigSet,
	}
}

func runConfigSet(cmd *cobra.Command, args []string) error {
	key := strings.ToLower(args[0])
	if !config.IsValidKey(key) {
		return fmt.Errorf("invalid config key: %s (valid keys: %s)", key, strings.Join(config.ValidKeys(), ", "))
	}

	var value string
	if len(args) == 2 {
		value = args[1]
	} else {
		var err error
		value, err = readValueInteractively(key)
		if err != nil {
			return err
		}
	}

	if err := config.Set(key, value); err != nil {
		return err
	}

	if key == "app_password" && !config.IsEnvReference(value) {
		fmt.Fprintln(os.Stderr, "Warning: Storing app_password directly in config file.")
		fmt.Fprintln(os.Stderr, "Consider using ${env:ATLAS_APP_PASSWORD} syntax instead.")
	}

	fmt.Printf("Set %s\n", key)
	return nil
}

func readValueInteractively(key string) (string, error) {
	stdinInfo, _ := os.Stdin.Stat()
	isPiped := (stdinInfo.Mode() & os.ModeCharDevice) == 0

	if isPiped {
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			return strings.TrimSpace(scanner.Text()), nil
		}
		if err := scanner.Err(); err != nil {
			return "", fmt.Errorf("failed to read from stdin: %w", err)
		}
		return "", fmt.Errorf("no value provided via stdin")
	}

	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("no value provided and stdin is not a terminal")
	}

	if key == "app_password" {
		fmt.Fprint(os.Stderr, "Enter app password: ")
		password, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", fmt.Errorf("failed to read password: %w", err)
		}
		return string(password), nil
	}

	fmt.Fprintf(os.Stderr, "Enter %s: ", key)
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		return strings.TrimSpace(scanner.Text()), nil
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("failed to read input: %w", err)
	}
	return "", fmt.Errorf("no input provided")
}

func newConfigGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <key>",
		Short: "Get a configuration value",
		Long: `Get a configuration value. Valid keys: workspace, username, app_password.

Use --verbose to see whether the value uses an environment variable reference.`,
		Args: cobra.ExactArgs(1),
		RunE: runConfigGet,
	}
	return cmd
}

func runConfigGet(cmd *cobra.Command, args []string) error {
	key := strings.ToLower(args[0])
	if !config.IsValidKey(key) {
		return fmt.Errorf("invalid config key: %s (valid keys: %s)", key, strings.Join(config.ValidKeys(), ", "))
	}

	rawValue, hasEnvRef, err := config.GetRaw(key)
	if err != nil {
		return err
	}

	if rawValue == "" {
		fmt.Printf("%s: (not set)\n", key)
		return nil
	}

	if verbose && hasEnvRef {
		fmt.Printf("%s: %s (env reference)\n", key, rawValue)
	} else if key == "app_password" {
		fmt.Printf("%s: ****\n", key)
	} else {
		fmt.Printf("%s: %s\n", key, rawValue)
	}

	return nil
}

func newConfigVerifyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "verify",
		Short: "Verify credentials by calling the Bitbucket API",
		Long:  "Verify that the configured credentials are valid by making a test API call.",
		Args:  cobra.NoArgs,
		RunE:  runConfigVerify,
	}
}

func runConfigVerify(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if cfg.Username == "" || cfg.AppPassword == "" {
		return fmt.Errorf("credentials not configured. Run 'atlas config set username' and 'atlas config set app_password'")
	}

	fmt.Println("Credentials loaded successfully.")
	fmt.Println("Note: Full verification requires the Bitbucket API client (Phase 2).")
	return nil
}
