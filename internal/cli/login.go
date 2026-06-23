package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/config"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const apiTokenURL = "https://id.atlassian.com/manage-profile/security/api-tokens"

var requiredBitbucketAPITokenScopes = []string{
	"read:user:bitbucket",
	"read:repository:bitbucket",
	"write:repository:bitbucket",
	"read:pullrequest:bitbucket",
	"write:pullrequest:bitbucket",
	"read:snippet:bitbucket",
	"write:snippet:bitbucket",
	"delete:snippet:bitbucket",
}

func newLoginCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Configure Bitbucket API token credentials",
		Long:  "Configure Bitbucket API token credentials and verify them against the Bitbucket API.",
		Args:  cobra.NoArgs,
		RunE:  runLogin,
	}
}

func runLogin(cmd *cobra.Command, args []string) error {
	fmt.Fprintf(cmd.OutOrStdout(), "Create a Bitbucket API token at:\n%s\n\n", apiTokenURL)
	fmt.Fprintln(cmd.OutOrStdout(), "Use these scopes:")
	for _, scope := range requiredBitbucketAPITokenScopes {
		fmt.Fprintf(cmd.OutOrStdout(), "%s\n", scope)
	}
	fmt.Fprintln(cmd.OutOrStdout())

	reader := bufio.NewReader(os.Stdin)
	username, err := readPromptedLine(cmd, reader, "Atlassian email: ")
	if err != nil {
		return err
	}

	apiToken, err := readPromptedSecret(cmd, reader, "API token: ")
	if err != nil {
		return err
	}

	client, err := bitbucket.NewClientWithCredentials(username, apiToken, bitbucket.WithNoCache(true))
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	user, err := client.GetCurrentUser()
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	if err := config.Set("username", username); err != nil {
		return err
	}
	if err := config.SaveAPIToken(apiToken); err != nil {
		return err
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Credentials saved.")
	fmt.Print(formatVerifiedUser(*user))
	return nil
}

func readPromptedLine(cmd *cobra.Command, reader *bufio.Reader, prompt string) (string, error) {
	fmt.Fprint(cmd.ErrOrStderr(), prompt)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("failed to read input: %w", err)
	}
	value := strings.TrimSpace(line)
	if value == "" {
		return "", fmt.Errorf("value cannot be empty")
	}
	return value, nil
}

func readPromptedSecret(cmd *cobra.Command, reader *bufio.Reader, prompt string) (string, error) {
	fmt.Fprint(cmd.ErrOrStderr(), prompt)
	if term.IsTerminal(int(os.Stdin.Fd())) {
		password, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(cmd.ErrOrStderr())
		if err != nil {
			return "", fmt.Errorf("failed to read API token: %w", err)
		}
		value := strings.TrimSpace(string(password))
		if value == "" {
			return "", fmt.Errorf("API token cannot be empty")
		}
		return value, nil
	}

	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("failed to read API token: %w", err)
	}
	value := strings.TrimSpace(line)
	if value == "" {
		return "", fmt.Errorf("API token cannot be empty")
	}
	return value, nil
}
