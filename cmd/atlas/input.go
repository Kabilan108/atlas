package main

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func readInputs(cmd *cobra.Command, args []string) ([]string, error) {
	if len(args) == 1 && args[0] == "-" {
		scanner := bufio.NewScanner(cmd.InOrStdin())
		var inputs []string
		for scanner.Scan() {
			candidate := strings.TrimSpace(scanner.Text())
			if candidate != "" {
				inputs = append(inputs, candidate)
			}
		}
		if err := scanner.Err(); err != nil {
			return nil, err
		}
		if len(inputs) == 0 {
			return nil, fmt.Errorf("no input provided via stdin")
		}
		return inputs, nil
	}

	if len(args) > 0 && args[0] == "-" {
		return nil, fmt.Errorf("'-' must be used alone to read from stdin")
	}

	var inputs []string
	for _, arg := range args {
		trimmed := strings.TrimSpace(arg)
		if trimmed != "" {
			inputs = append(inputs, trimmed)
		}
	}
	if len(inputs) == 0 {
		return nil, fmt.Errorf("no inputs provided")
	}
	return inputs, nil
}
