package cli

import (
	"fmt"
	"strings"

	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/pipeline"
	"github.com/spf13/cobra"
)

func newPipelineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pipeline",
		Short: "Work with Bitbucket Pipelines runs",
	}
	cmd.AddCommand(newPipelineListCmd())
	cmd.AddCommand(newPipelineViewCmd())
	cmd.AddCommand(newPipelineLogCmd())
	cmd.AddCommand(newPipelineRunCmd())
	cmd.AddCommand(newPipelineStopCmd())
	return cmd
}

func pipelineRepo(cmd *cobra.Command, env *cliEnv) (ops.RepoRef, error) {
	repo, err := env.repository(cmd)
	if err != nil {
		return ops.RepoRef{}, err
	}
	return ops.RepoRef{Repo: repo}, nil
}

func newPipelineListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List recent pipeline runs",
		Args:  cobra.NoArgs,
	}
	addPRRepoFlag(cmd)
	cmd.Flags().StringP("branch", "b", "", "Only runs on this branch")
	cmd.Flags().String("commit", "", "Only runs on this commit (full SHA)")
	cmd.Flags().String("pattern", "", "Only runs of this selector pattern, e.g. a custom pipeline name")
	cmd.Flags().IntP("limit", "L", 20, "Maximum number of runs (max 100)")
	return bind(cmd, binding[pipeline.ListInput, *pipeline.ListResult]{
		op: pipeline.List,
		input: func(cmd *cobra.Command, _ []string, env *cliEnv) (pipeline.ListInput, error) {
			repo, err := pipelineRepo(cmd, env)
			if err != nil {
				return pipeline.ListInput{}, err
			}
			branch, _ := cmd.Flags().GetString("branch")
			commit, _ := cmd.Flags().GetString("commit")
			pattern, _ := cmd.Flags().GetString("pattern")
			limit, _ := cmd.Flags().GetInt("limit")
			return pipeline.ListInput{RepoRef: repo, Branch: branch, Commit: commit, Pattern: pattern, Limit: limit}, nil
		},
	})
}

func newPipelineViewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "view <build>...",
		Short: "Show pipeline runs and their steps",
		Long:  "Show pipeline runs and their steps. A build is a build number (2090 or #2090) or a pipeline UUID.",
		Args:  cobra.RangeArgs(1, 10),
	}
	addPRRepoFlag(cmd)
	return bind(cmd, binding[pipeline.ViewInput, *pipeline.ViewResult]{
		op: pipeline.View,
		input: func(cmd *cobra.Command, args []string, env *cliEnv) (pipeline.ViewInput, error) {
			repo, err := pipelineRepo(cmd, env)
			if err != nil {
				return pipeline.ViewInput{}, err
			}
			return pipeline.ViewInput{RepoRef: repo, Builds: args}, nil
		},
	})
}

func newPipelineLogCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "log <build>...",
		Short: "Show step logs of pipeline runs",
		Long:  "Show the tail of step logs: failed steps by default, else running ones, or the steps named with --step.",
		Args:  cobra.RangeArgs(1, 10),
	}
	addPRRepoFlag(cmd)
	cmd.Flags().StringArray("step", nil, "Step name or UUID; repeatable")
	cmd.Flags().Int("tail", 0, "Bytes from the end of each step log (default 12000)")
	cmd.Flags().Int("max-bytes", 0, "Cap across all logs (default 48000)")
	cmd.Flags().Bool("full", false, "Print up to 2 MiB of each step log, 8 MiB in all")
	cmd.MarkFlagsMutuallyExclusive("full", "tail")
	cmd.MarkFlagsMutuallyExclusive("full", "max-bytes")
	return bind(cmd, binding[pipeline.LogInput, *pipeline.LogResult]{
		op: pipeline.Log,
		input: func(cmd *cobra.Command, args []string, env *cliEnv) (pipeline.LogInput, error) {
			repo, err := pipelineRepo(cmd, env)
			if err != nil {
				return pipeline.LogInput{}, err
			}
			steps, _ := cmd.Flags().GetStringArray("step")
			tail, _ := cmd.Flags().GetInt("tail")
			maxBytes, _ := cmd.Flags().GetInt("max-bytes")
			if full, _ := cmd.Flags().GetBool("full"); full {
				tail, maxBytes = pipeline.MaxTailBytes, pipeline.MaxCallBytes
			}
			return pipeline.LogInput{RepoRef: repo, Builds: args, Steps: steps, TailBytes: tail, MaxBytes: maxBytes}, nil
		},
	})
}

func newPipelineRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Start a pipeline run",
		Long:  "Start a pipeline run on a branch or commit. With --wait, exits non-zero unless the run finished successfully in time.",
		Args:  cobra.NoArgs,
	}
	addPRRepoFlag(cmd)
	cmd.Flags().StringP("branch", "b", "", "Branch to run on")
	cmd.Flags().String("commit", "", "Commit to run on (full SHA); with --branch, pins the run")
	cmd.Flags().StringP("pipeline", "p", "", "Custom pipeline name from bitbucket-pipelines.yml")
	cmd.Flags().StringArray("var", nil, "Pipeline variable KEY=VALUE; repeatable")
	cmd.Flags().StringArray("secured-var", nil, "Secured pipeline variable KEY=VALUE; repeatable")
	cmd.Flags().Duration("wait", 0, "Wait this long for the run to finish (max 10m)")
	return bind(cmd, binding[pipeline.RunInput, *pipeline.RunReceipt]{
		op:    pipeline.Start,
		input: pipelineRunInput,
		after: func(cmd *cobra.Command, receipt *pipeline.RunReceipt) error {
			if wait, _ := cmd.Flags().GetDuration("wait"); wait == 0 {
				return nil
			}
			switch {
			case !receipt.Finished:
				return pending(fmt.Errorf("pipeline #%d is still %s; it keeps running", receipt.BuildNumber, receipt.Status))
			case receipt.Status != "SUCCESSFUL":
				return fmt.Errorf("pipeline #%d finished with %s", receipt.BuildNumber, receipt.Status)
			}
			return nil
		},
	})
}

func pipelineRunInput(cmd *cobra.Command, _ []string, env *cliEnv) (pipeline.RunInput, error) {
	repo, err := pipelineRepo(cmd, env)
	if err != nil {
		return pipeline.RunInput{}, err
	}
	branch, _ := cmd.Flags().GetString("branch")
	commit, _ := cmd.Flags().GetString("commit")
	custom, _ := cmd.Flags().GetString("pipeline")
	wait, _ := cmd.Flags().GetDuration("wait")
	seconds, err := waitSeconds(wait)
	if err != nil {
		return pipeline.RunInput{}, err
	}
	in := pipeline.RunInput{RepoRef: repo, Branch: branch, Commit: commit, Pipeline: custom, WaitSeconds: seconds}
	for _, flag := range []struct {
		name    string
		secured bool
	}{{"var", false}, {"secured-var", true}} {
		values, _ := cmd.Flags().GetStringArray(flag.name)
		for i, value := range values {
			key, val, ok := strings.Cut(value, "=")
			if !ok || key == "" {
				if flag.secured {
					return pipeline.RunInput{}, fmt.Errorf("--%s #%d: expected KEY=VALUE (value not shown)", flag.name, i+1)
				}
				return pipeline.RunInput{}, fmt.Errorf("--%s %q: expected KEY=VALUE", flag.name, value)
			}
			in.Variables = append(in.Variables, pipeline.Variable{Key: key, Value: val, Secured: flag.secured})
		}
	}
	return in, nil
}

func newPipelineStopCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stop <build>",
		Short: "Stop a running pipeline",
		Args:  cobra.ExactArgs(1),
	}
	addPRRepoFlag(cmd)
	return bind(cmd, binding[pipeline.StopInput, *pipeline.StopReceipt]{
		op: pipeline.Stop,
		input: func(cmd *cobra.Command, args []string, env *cliEnv) (pipeline.StopInput, error) {
			repo, err := pipelineRepo(cmd, env)
			if err != nil {
				return pipeline.StopInput{}, err
			}
			return pipeline.StopInput{RepoRef: repo, Build: args[0]}, nil
		},
	})
}
