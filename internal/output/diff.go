package output

import (
	"bufio"
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type DiffHunk struct {
	Header    string
	OldStart  int
	OldCount  int
	NewStart  int
	NewCount  int
	Lines     []string
	FilePath  string
}

type DiffParser struct {
	hunks map[string][]DiffHunk
}

func NewDiffParser() *DiffParser {
	return &DiffParser{
		hunks: make(map[string][]DiffHunk),
	}
}

var hunkHeaderRegex = regexp.MustCompile(`^@@\s+-(\d+)(?:,(\d+))?\s+\+(\d+)(?:,(\d+))?\s+@@(.*)$`)

func (p *DiffParser) Parse(diff []byte) error {
	scanner := bufio.NewScanner(bytes.NewReader(diff))
	var currentFile string
	var currentHunk *DiffHunk

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "diff --git") {
			if currentHunk != nil && currentFile != "" {
				p.hunks[currentFile] = append(p.hunks[currentFile], *currentHunk)
			}
			currentHunk = nil
			parts := strings.Split(line, " b/")
			if len(parts) >= 2 {
				currentFile = parts[1]
			}
			continue
		}

		if strings.HasPrefix(line, "@@") {
			if currentHunk != nil && currentFile != "" {
				p.hunks[currentFile] = append(p.hunks[currentFile], *currentHunk)
			}

			matches := hunkHeaderRegex.FindStringSubmatch(line)
			if matches == nil {
				continue
			}

			oldStart, _ := strconv.Atoi(matches[1])
			oldCount := 1
			if matches[2] != "" {
				oldCount, _ = strconv.Atoi(matches[2])
			}
			newStart, _ := strconv.Atoi(matches[3])
			newCount := 1
			if matches[4] != "" {
				newCount, _ = strconv.Atoi(matches[4])
			}

			currentHunk = &DiffHunk{
				Header:   line,
				OldStart: oldStart,
				OldCount: oldCount,
				NewStart: newStart,
				NewCount: newCount,
				FilePath: currentFile,
			}
			continue
		}

		if currentHunk != nil && (strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-") || strings.HasPrefix(line, " ")) {
			currentHunk.Lines = append(currentHunk.Lines, line)
		}
	}

	if currentHunk != nil && currentFile != "" {
		p.hunks[currentFile] = append(p.hunks[currentFile], *currentHunk)
	}

	return scanner.Err()
}

func (p *DiffParser) GetHunkForLine(filePath string, lineNum int) *DiffHunk {
	hunks, ok := p.hunks[filePath]
	if !ok {
		return nil
	}

	for _, hunk := range hunks {
		if p.hunkContainsLine(hunk, lineNum) {
			return &hunk
		}
	}

	return nil
}

func (p *DiffParser) hunkContainsLine(hunk DiffHunk, lineNum int) bool {
	newLine := hunk.NewStart
	oldLine := hunk.OldStart

	for _, line := range hunk.Lines {
		if strings.HasPrefix(line, "+") {
			if newLine == lineNum {
				return true
			}
			newLine++
		} else if strings.HasPrefix(line, "-") {
			if oldLine == lineNum {
				return true
			}
			oldLine++
		} else {
			if newLine == lineNum || oldLine == lineNum {
				return true
			}
			newLine++
			oldLine++
		}
	}

	return false
}

func (h *DiffHunk) FormatContext(targetLine int, contextLines int) string {
	var sb strings.Builder

	sb.WriteString("```diff\n")
	sb.WriteString(h.Header)
	sb.WriteString("\n")

	startIdx, endIdx := h.findContextRange(targetLine, contextLines)
	for i := startIdx; i <= endIdx && i < len(h.Lines); i++ {
		sb.WriteString(h.Lines[i])
		sb.WriteString("\n")
	}

	sb.WriteString("```\n")
	return sb.String()
}

func (h *DiffHunk) findContextRange(targetLine int, contextLines int) (int, int) {
	targetIdx := -1
	newLine := h.NewStart

	for i, line := range h.Lines {
		if strings.HasPrefix(line, "+") {
			if newLine == targetLine {
				targetIdx = i
				break
			}
			newLine++
		} else if strings.HasPrefix(line, "-") {
			// Skip deleted lines for new line counting
		} else {
			if newLine == targetLine {
				targetIdx = i
				break
			}
			newLine++
		}
	}

	if targetIdx < 0 {
		return 0, len(h.Lines) - 1
	}

	start := targetIdx - contextLines
	if start < 0 {
		start = 0
	}
	end := targetIdx + contextLines
	if end >= len(h.Lines) {
		end = len(h.Lines) - 1
	}

	return start, end
}

func FormatDiffContext(diff []byte, filePath string, lineNum int) string {
	parser := NewDiffParser()
	if err := parser.Parse(diff); err != nil {
		return ""
	}

	hunk := parser.GetHunkForLine(filePath, lineNum)
	if hunk == nil {
		return ""
	}

	return hunk.FormatContext(lineNum, 3)
}

func FormatFileLineHeader(path string, line int) string {
	if line > 0 {
		return fmt.Sprintf("#### `%s:%d`\n", path, line)
	}
	return fmt.Sprintf("#### `%s`\n", path)
}
