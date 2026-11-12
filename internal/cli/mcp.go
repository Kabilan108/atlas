package cli

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/kabilan108/atlas/internal/mcpserver"
	"github.com/spf13/cobra"
)

const (
	mcpWorkspaceEnv    = "ATLAS_MCP_WORKSPACE"
	mcpJiraCloudIDEnv  = "ATLAS_MCP_JIRA_CLOUD_ID"
	mcpAllowedHostsEnv = "ATLAS_MCP_ALLOWED_HOSTS"
)

func newMCPCmd(version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run Atlas as an MCP server",
	}
	cmd.AddCommand(newMCPServeCmd(version))
	return cmd
}

func newMCPServeCmd(version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve Atlas tools over stateless Streamable HTTP",
		Long: fmt.Sprintf(`Serve Atlas tools at /mcp over stateless Streamable HTTP.

Each request carries its own credentials in headers: %s, plus
%s for Bitbucket tools and %s for Jira tools.
The server stores no credentials, keeps no sessions, and never uses the disk
cache. Request logs go to stderr and never include headers. Request bodies
are capped at %d MiB, enough for jira_attach's largest upload, and must
arrive within %s.`, mcpserver.HeaderEmail, mcpserver.HeaderBitbucketToken, mcpserver.HeaderJiraToken,
			mcpserver.DefaultMaxBodyBytes>>20, mcpserver.DefaultBodyReadTimeout),
		Args: cobra.NoArgs,
		RunE: runMCPServe(version),
	}
	cmd.Flags().String("addr", "127.0.0.1:8080", "Address to listen on")
	cmd.Flags().String("workspace", "", "Default Bitbucket workspace for repo arguments without one (env "+mcpWorkspaceEnv+", default "+mcpserver.DefaultWorkspace+")")
	cmd.Flags().String("jira-cloud-id", "", "Jira cloud ID behind the Atlassian API gateway (env "+mcpJiraCloudIDEnv+", default "+mcpserver.DefaultJiraCloudID+")")
	cmd.Flags().StringArray("allowed-host", nil, "Also accept this Host header on a loopback address, e.g. the name a local reverse proxy forwards; repeatable (env "+mcpAllowedHostsEnv+", comma-separated)")
	return cmd
}

func runMCPServe(version string) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		addr, _ := cmd.Flags().GetString("addr")
		workspace := flagOrEnv(cmd, "workspace", mcpWorkspaceEnv, mcpserver.DefaultWorkspace)
		jiraCloudID := flagOrEnv(cmd, "jira-cloud-id", mcpJiraCloudIDEnv, mcpserver.DefaultJiraCloudID)
		allowedHosts, _ := cmd.Flags().GetStringArray("allowed-host")
		if !cmd.Flags().Changed("allowed-host") {
			allowedHosts = splitList(os.Getenv(mcpAllowedHostsEnv))
		}
		logger := slog.New(slog.NewJSONHandler(cmd.ErrOrStderr(), nil))
		handler, err := mcpserver.Handler(mcpserver.Options{
			Version:          version,
			DefaultWorkspace: workspace,
			JiraCloudID:      jiraCloudID,
			AllowedHosts:     allowedHosts,
			Logger:           logger,
		})
		if err != nil {
			return err
		}
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			return err
		}
		// The handler bounds body reads itself (BodyReadTimeout).
		server := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		served := make(chan error, 1)
		go func() { served <- server.Serve(listener) }()
		logger.Info("listening", "url", "http://"+listener.Addr().String()+"/mcp", "default_workspace", workspace, "jira_cloud_id", jiraCloudID)
		select {
		case err := <-served:
			return err
		case <-ctx.Done():
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	}
}

// flagOrEnv returns the flag's value when it was set, else the environment
// variable, else fallback.
func flagOrEnv(cmd *cobra.Command, flag, env, fallback string) string {
	if cmd.Flags().Changed(flag) {
		value, _ := cmd.Flags().GetString(flag)
		return value
	}
	if value := os.Getenv(env); value != "" {
		return value
	}
	return fallback
}

func splitList(value string) []string {
	var items []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
}
