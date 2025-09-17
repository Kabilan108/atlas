package main

import (
	"fmt"
	"os"
	"sync"

	"github.com/kabilan108/atlas/internal/config"
	"github.com/kabilan108/atlas/internal/document"
	"github.com/kabilan108/atlas/internal/httpclient"
	"github.com/kabilan108/atlas/internal/output"
	"github.com/spf13/cobra"
)

const (
	wrapFenced  = "fenced"
	wrapXMLish  = "xmlish"
	defaultWrap = wrapFenced
)

var (
	wrapMode    string
	concurrency int
	verbose     bool

	runtime runtimeState
	outMu   sync.Mutex
)

type runtimeState struct {
	config      config.Config
	httpClient  *httpclient.Client
	outputForm  output.Format
	concurrency int
	verbose     bool
}

var rootCmd = &cobra.Command{
	Use:           "atlas",
	Short:         "Fetch Atlassian content and emit markdown-wrapped output",
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if shouldSkipInit(cmd) {
			return nil
		}

		if wrapMode != wrapFenced && wrapMode != wrapXMLish {
			return fmt.Errorf("invalid wrap mode %q: expected %s or %s", wrapMode, wrapFenced, wrapXMLish)
		}
		if concurrency <= 0 {
			return fmt.Errorf("concurrency must be greater than zero")
		}

		runtime.outputForm = output.Format(wrapMode)
		runtime.concurrency = concurrency
		runtime.verbose = verbose

		cfg, err := config.LoadConfig()
		if err != nil {
			return err
		}
		runtime.config = cfg

		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&wrapMode, "wrap", defaultWrap, "Output wrapping mode (fenced|xmlish)")
	rootCmd.PersistentFlags().IntVar(&concurrency, "concurrency", httpclient.DefaultConcurrency, "Maximum concurrent requests")
	rootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "Enable verbose logging")

	rootCmd.AddCommand(newConfluenceCmd())
	rootCmd.AddCommand(newBitbucketCmd())
	rootCmd.AddCommand(newGetCmd())
	rootCmd.AddCommand(newVersionCmd())
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func shouldSkipInit(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	if cmd.Annotations != nil && cmd.Annotations["skipInit"] == "true" {
		return true
	}
	return false
}

func printDocument(doc document.Document) error {
	outMu.Lock()
	defer outMu.Unlock()

	switch runtime.outputForm {
	case output.Fenced:
		return output.PrintFenced(doc)
	case output.XMLish:
		return output.PrintXMLish(doc)
	default:
		return fmt.Errorf("unsupported output format: %s", runtime.outputForm)
	}
}

func getHTTPClient() (*httpclient.Client, error) {
	if runtime.httpClient != nil {
		return runtime.httpClient, nil
	}
	client, err := httpclient.New()
	if err != nil {
		return nil, err
	}
	runtime.httpClient = client
	return client, nil
}

func verbosef(format string, args ...interface{}) {
	if !runtime.verbose {
		return
	}
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}
