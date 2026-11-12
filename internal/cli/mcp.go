package cli

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kabilan108/atlas/internal/mcpserver"
	"github.com/spf13/cobra"
)

const mcpWorkspaceEnv = "ATLAS_MCP_WORKSPACE"

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

Each request carries its own credentials in the %s and %s headers.
The server stores no credentials, keeps no sessions, and never uses the disk
cache. Request logs go to stderr and never include headers.`, mcpserver.HeaderEmail, mcpserver.HeaderBitbucketToken),
		Args: cobra.NoArgs,
		RunE: runMCPServe(version),
	}
	cmd.Flags().String("addr", "127.0.0.1:8080", "Address to listen on")
	cmd.Flags().String("workspace", "", "Default Bitbucket workspace for repo arguments without one (env "+mcpWorkspaceEnv+")")
	return cmd
}

func runMCPServe(version string) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		addr, _ := cmd.Flags().GetString("addr")
		workspace, _ := cmd.Flags().GetString("workspace")
		if !cmd.Flags().Changed("workspace") {
			workspace = os.Getenv(mcpWorkspaceEnv)
		}
		logger := slog.New(slog.NewJSONHandler(cmd.ErrOrStderr(), nil))
		handler, err := mcpserver.Handler(mcpserver.Options{
			Version:          version,
			DefaultWorkspace: workspace,
			Logger:           logger,
		})
		if err != nil {
			return err
		}
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			return err
		}
		server := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		served := make(chan error, 1)
		go func() { served <- server.Serve(listener) }()
		logger.Info("listening", "url", "http://"+listener.Addr().String()+"/mcp", "default_workspace", workspace)
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
