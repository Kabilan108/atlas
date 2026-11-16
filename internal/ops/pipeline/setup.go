package pipeline

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/kabilan108/atlas/internal/bitbucket"
)

// setupAllowance is how far past the tail a log read reaches unless
// include_setup is set, so that a log a little longer than the tail still
// arrives whole and its runner setup is cut rather than counted.
const setupAllowance = 32 << 10

const (
	maxSetupImages  = 10
	maxSetupNotices = 10
	maxSetupNotice  = 300
)

// Setup is the runner setup cut from the start of a step log: everything
// Bitbucket printed before the step's first script command. Runner is the
// runner's name, or the runtime ("cloud") when Bitbucket names none.
// Notices are setup lines that read like a problem, such as a cache that
// was not found; they are kept, since one may explain a later failure.
type Setup struct {
	OmittedBytes     int           `json:"omitted_bytes"`
	Runner           string        `json:"runner,omitempty"`
	Images           []SetupImage  `json:"images,omitempty"`
	Variables        []RunVariable `json:"variables,omitempty"`
	VariablesOmitted int           `json:"variables_omitted,omitempty"`
	Notices          []string      `json:"notices,omitempty"`
}

// SetupImage is one "Images used" entry: the build image or a service's.
type SetupImage struct {
	Name string `json:"name"`
	Ref  string `json:"ref"`
}

// setupMarker is a line every runner setup prints after the clone and
// before the script. The script's first command counts only after it, so a
// setup command that reads like the script's first line cannot end the cut.
const setupMarker = "Default variables:"

// setupEnd is the offset of the line that echoes the step's first script
// command. That must be the first command echoed after the setup marker;
// a later match could be the script's own output (bash -x prints the same
// form), so anything else reports false. A step that failed in setup never
// echoes its script, so its log is left whole too.
func setupEnd(content []byte, commands []bitbucket.PipelineCommand) (int, bool) {
	anchor, ok := scriptAnchor(commands)
	if !ok {
		return 0, false
	}
	pastMarker := false
	for offset := 0; offset < len(content); {
		end := len(content)
		if newline := bytes.IndexByte(content[offset:], '\n'); newline >= 0 {
			end = offset + newline + 1
		}
		switch line := logLine(content[offset:end]); {
		case line == setupMarker:
			pastMarker = true
		case pastMarker && strings.HasPrefix(line, "+ "):
			return offset, line == anchor
		}
		offset = end
	}
	return 0, false
}

// scriptAnchor is the log line that starts the script. Bitbucket echoes a
// multi-line command as "+ " and its first line, then the rest as is.
func scriptAnchor(commands []bitbucket.PipelineCommand) (string, bool) {
	if len(commands) == 0 {
		return "", false
	}
	first, _, _ := strings.Cut(strings.TrimSpace(commands[0].Command), "\n")
	first = strings.TrimRight(first, " \t\r")
	if first == "" {
		return "", false
	}
	return "+ " + first, true
}

func logLine(raw []byte) string {
	return strings.TrimRight(ansiEscape.ReplaceAllString(string(raw), ""), " \t\r\n")
}

var (
	// setupVariable is a "Pipeline variables" entry: "KEY: value", or a
	// bare KEY with its value withheld.
	setupVariable = regexp.MustCompile(`^    ([A-Za-z_][A-Za-z0-9_]*)(:(?: (.*))?)?$`)
	setupImage    = regexp.MustCompile(`^\s+(\S+?)\s*:\s+(\S.*)$`)
	// maskedValue is how a secured value can read once Bitbucket masks it:
	// the variable's name ($KEY), asterisks, or a placeholder.
	maskedValue  = regexp.MustCompile(`^(?:\$\{?[A-Za-z_][A-Za-z0-9_]*\}?|\*+|[\[<(]?(?i:masked|secured|secret|redacted|hidden)[\]>)]?)$`)
	setupProblem = regexp.MustCompile(`(?i)\b(?:errors?|fail(?:ed|ure)?|fatal|denied|not found|unable|warning|cannot|could not|timed out|timeout)\b`)
	imageDigest  = regexp.MustCompile(`(@sha256:[0-9a-f]{12})[0-9a-f]{52}\b`)
)

// parseSetup reads the runner, images and pipeline variables from a cut
// setup. Any value that looks masked, or is missing, is treated as secured
// and never kept.
func parseSetup(preamble string, omitted int) *Setup {
	setup := &Setup{OmittedBytes: omitted}
	var variables []RunVariable
	section, runtime := "", ""
	for _, line := range strings.Split(preamble, "\n") {
		line = strings.TrimRight(line, " \t\r")
		indented := line != "" && (line[0] == ' ' || line[0] == '\t')
		switch {
		case line == "":
			section = ""
			continue
		case !indented && strings.HasSuffix(line, ":"):
			section = line
			continue
		case !indented:
			section = ""
		}
		if !strings.HasSuffix(section, "variables:") && !strings.HasPrefix(line, "HEAD is now at ") &&
			setupProblem.MatchString(line) && len(setup.Notices) < maxSetupNotices {
			notice, _ := truncate(line, maxSetupNotice)
			setup.Notices = append(setup.Notices, notice)
		}
		switch {
		case !indented:
			if name, ok := strings.CutPrefix(line, "Runner name:"); ok {
				setup.Runner, _ = truncate(strings.TrimSpace(name), maxVariableKey)
			}
		case section == "Pipeline variables:":
			variables = addSetupVariable(variables, line)
		case section == "Images used:" && len(setup.Images) < maxSetupImages:
			if m := setupImage.FindStringSubmatch(line); m != nil {
				name, _ := truncate(m[1], maxVariableKey)
				ref, _ := truncate(m[2], maxVariableValue)
				setup.Images = append(setup.Images, SetupImage{Name: name, Ref: ref})
			}
		case section == "Runtime:" && runtime == "":
			runtime = strings.TrimSuffix(strings.TrimSpace(line), ":")
		}
	}
	if setup.Runner == "" {
		setup.Runner, _ = truncate(runtime, maxVariableKey)
	}
	setup.Variables, setup.VariablesOmitted = runVariables(variables)
	return setup
}

// addSetupVariable adds one "Pipeline variables" line. A line that is not
// an entry continues the previous value, which is then marked truncated.
func addSetupVariable(variables []RunVariable, line string) []RunVariable {
	m := setupVariable.FindStringSubmatch(line)
	if m == nil {
		if n := len(variables); n > 0 {
			variables[n-1].Truncated = true
		}
		return variables
	}
	key, hasValue, value := m[1], m[2] != "", strings.TrimSpace(m[3])
	if !hasValue || maskedValue.MatchString(value) {
		return append(variables, RunVariable{Key: key, Secured: true})
	}
	return append(variables, RunVariable{Key: key, Value: value})
}

// line is the one line that stands in for the cut setup.
func (s *Setup) line() string {
	parts := []string{"runner setup omitted: " + formatSize(s.OmittedBytes)}
	if s.Runner != "" {
		parts = append(parts, "runner "+oneLine(s.Runner))
	}
	switch {
	case len(s.Images) == 1 && s.Images[0].Name == "build":
		parts = append(parts, "image "+shortRef(s.Images[0].Ref))
	case len(s.Images) > 0:
		images := make([]string, len(s.Images))
		for i, image := range s.Images {
			images[i] = oneLine(image.Name) + "=" + shortRef(image.Ref)
		}
		parts = append(parts, "images "+strings.Join(images, ", "))
	}
	if len(s.Variables) > 0 || s.VariablesOmitted > 0 {
		variables := make([]string, 0, len(s.Variables)+1)
		for _, v := range s.Variables {
			variables = append(variables, v.label())
		}
		if s.VariablesOmitted > 0 {
			variables = append(variables, fmt.Sprintf("and %d more", s.VariablesOmitted))
		}
		parts = append(parts, "variables "+strings.Join(variables, " "))
	}
	parts = append(parts, "include_setup shows it")
	return "[" + strings.Join(parts, " · ") + "]"
}

// text is what stands in for the cut setup: its line, then its notices.
func (s *Setup) text() string {
	lines := append([]string{s.line()}, s.Notices...)
	return strings.Join(lines, "\n") + "\n"
}

// shortRef keeps the first 12 hex digits of an image digest.
func shortRef(ref string) string {
	return imageDigest.ReplaceAllString(oneLine(ref), "$1")
}

func (v RunVariable) label() string {
	key := oneLine(v.Key)
	if v.Secured {
		return key + " (secured)"
	}
	label := key + "=" + quoteIfNeeded(oneLine(v.Value))
	if v.Truncated {
		label += " (truncated)"
	}
	return label
}

func quoteIfNeeded(value string) string {
	if value == "" || strings.ContainsAny(value, " \"·[]") {
		return strconv.Quote(value)
	}
	return value
}

func formatSize(n int) string {
	if n < 1024 {
		return fmt.Sprintf("%d bytes", n)
	}
	return fmt.Sprintf("%.1f KB", float64(n)/1024)
}
