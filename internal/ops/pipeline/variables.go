package pipeline

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/kabilan108/atlas/internal/bitbucket"
)

// Bounds on the variables one run shows, so a run cannot flood the output.
const (
	maxRunVariables  = 25
	maxVariableKey   = 100
	maxVariableValue = 200
)

// RunVariable is a variable a run was started with. A secured variable has
// no value here, whatever Bitbucket returns. Truncated is set when the key
// or value was cut to its cap.
type RunVariable struct {
	Key       string `json:"key"`
	Value     string `json:"value,omitempty"`
	Secured   bool   `json:"secured,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

// runVariables keeps the first maxRunVariables variables that have a key
// and counts the rest.
func runVariables(variables []bitbucket.PipelineVariable) ([]RunVariable, int) {
	var kept []RunVariable
	omitted := 0
	for _, v := range variables {
		if v.Key == "" {
			continue
		}
		if len(kept) == maxRunVariables {
			omitted++
			continue
		}
		key, keyCut := truncate(v.Key, maxVariableKey)
		out := RunVariable{Key: key, Secured: v.Secured, Truncated: keyCut}
		if !v.Secured {
			value, valueCut := truncate(v.Value, maxVariableValue)
			out.Value, out.Truncated = value, keyCut || valueCut
		}
		kept = append(kept, out)
	}
	return kept, omitted
}

// truncate cuts s to at most limit bytes on a rune boundary.
func truncate(s string, limit int) (string, bool) {
	if len(s) <= limit {
		return s, false
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut], true
}

func writeVariables(b *strings.Builder, variables []RunVariable, omitted int) {
	if len(variables) == 0 && omitted == 0 {
		return
	}
	b.WriteString("\nVariables:\n")
	for _, v := range variables {
		more := ""
		if v.Truncated {
			more = " (truncated)"
		}
		if v.Secured {
			fmt.Fprintf(b, "- %s (secured)%s\n", oneLine(v.Key), more)
			continue
		}
		fmt.Fprintf(b, "- %s=%s%s\n", oneLine(v.Key), oneLine(v.Value), more)
	}
	if omitted > 0 {
		fmt.Fprintf(b, "- and %d more not shown\n", omitted)
	}
}
