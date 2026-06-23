package update

import "testing"

func TestCompareVersions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a    string
		b    string
		want int
	}{
		{name: "equal", a: "0.0.10", b: "v0.0.10", want: 0},
		{name: "less", a: "0.0.9", b: "0.0.10", want: -1},
		{name: "greater", a: "0.1.0", b: "0.0.10", want: 1},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := CompareVersions(tt.a, tt.b)
			if err != nil {
				t.Fatalf("CompareVersions() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("CompareVersions() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestParseVersionRejectsDev(t *testing.T) {
	t.Parallel()

	if _, err := ParseVersion("dev"); err == nil {
		t.Fatal("ParseVersion() error = nil, want error")
	}
}
