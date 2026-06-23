package update

import (
	"fmt"
	"strconv"
	"strings"
)

type Version struct {
	Major int
	Minor int
	Patch int
}

func ParseVersion(raw string) (Version, error) {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "v")
	if s == "" {
		return Version{}, fmt.Errorf("version is empty")
	}

	main, _, _ := strings.Cut(s, "-")
	parts := strings.Split(main, ".")
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("version %q must use major.minor.patch", raw)
	}

	values := make([]int, len(parts))
	for i, part := range parts {
		if part == "" {
			return Version{}, fmt.Errorf("version %q has an empty component", raw)
		}
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 {
			return Version{}, fmt.Errorf("version %q has an invalid component %q", raw, part)
		}
		values[i] = value
	}

	return Version{Major: values[0], Minor: values[1], Patch: values[2]}, nil
}

func CompareVersions(a, b string) (int, error) {
	av, err := ParseVersion(a)
	if err != nil {
		return 0, err
	}
	bv, err := ParseVersion(b)
	if err != nil {
		return 0, err
	}

	switch {
	case av.Major != bv.Major:
		return compareInt(av.Major, bv.Major), nil
	case av.Minor != bv.Minor:
		return compareInt(av.Minor, bv.Minor), nil
	default:
		return compareInt(av.Patch, bv.Patch), nil
	}
}

func compareInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
