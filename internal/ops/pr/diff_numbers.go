package pr

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
)

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// numberDiff prefixes every line inside a hunk with its line number in the
// old and the new file, blank on the side the line is absent from, so an
// inline comment can name the line. File and hunk headers are left as they
// are.
func numberDiff(diff []byte) []byte {
	lines := bytes.SplitAfter(diff, []byte("\n"))
	width := 1
	for _, line := range lines {
		if m := hunkHeader.FindSubmatch(line); m != nil {
			width = max(width, len(strconv.Itoa(hunkEnd(m[1], m[2]))), len(strconv.Itoa(hunkEnd(m[3], m[4]))))
		}
	}
	var out bytes.Buffer
	out.Grow(len(diff) + len(lines)*(2*width+3))
	var oldLine, newLine, oldLeft, newLeft int
	inHunk := false
	for i, line := range lines {
		if len(line) == 0 {
			continue
		}
		if m := hunkHeader.FindSubmatch(line); m != nil {
			oldLine, oldLeft = atoi(m[1]), count(m[2])
			newLine, newLeft = atoi(m[3]), count(m[4])
			inHunk = true
			out.Write(line)
			continue
		}
		if !inHunk {
			out.Write(line)
			continue
		}
		old, new := "", ""
		switch line[0] {
		case ' ':
			old, new = strconv.Itoa(oldLine), strconv.Itoa(newLine)
			oldLine, newLine, oldLeft, newLeft = oldLine+1, newLine+1, oldLeft-1, newLeft-1
		case '-':
			old = strconv.Itoa(oldLine)
			oldLine, oldLeft = oldLine+1, oldLeft-1
		case '+':
			new = strconv.Itoa(newLine)
			newLine, newLeft = newLine+1, newLeft-1
		case '\\':
		default:
			inHunk = false
			out.Write(line)
			continue
		}
		fmt.Fprintf(&out, "%*s %*s ", width, old, width, new)
		out.Write(line)
		if oldLeft <= 0 && newLeft <= 0 {
			// Past its counted lines a hunk holds only the "\ No newline at
			// end of file" marker.
			inHunk = i+1 < len(lines) && bytes.HasPrefix(lines[i+1], []byte(`\`))
		}
	}
	return out.Bytes()
}

func hunkEnd(start, length []byte) int {
	return atoi(start) + count(length)
}

func atoi(b []byte) int {
	n, _ := strconv.Atoi(string(b))
	return n
}

// count is a hunk range's length, which git leaves out when it is 1.
func count(b []byte) int {
	if len(b) == 0 {
		return 1
	}
	return atoi(b)
}
