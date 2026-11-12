package bitbucket

import "testing"

func TestParsePipelineResultURL(t *testing.T) {
	for raw, want := range map[string]PipelineResult{
		"https://bitbucket.org/acme/app/pipelines/results/2090":                      {"acme", "app", 2090},
		"https://bitbucket.org/acme/app/pipelines/results/2090/steps/%7Babc%7D":      {"acme", "app", 2090},
		"https://bitbucket.org/acme/app/addon/pipelines/home#!/results/17":           {"acme", "app", 17},
		"https://bitbucket.org/acme/app/addon/pipelines/home#!/results/17/steps/{x}": {"acme", "app", 17},
		" https://bitbucket.org/acme/app/pipelines/results/5 ":                       {"acme", "app", 5},
	} {
		got, ok := ParsePipelineResultURL(raw)
		if !ok || got != want {
			t.Errorf("ParsePipelineResultURL(%q) = %+v, %t; want %+v", raw, got, ok, want)
		}
	}
	for _, raw := range []string{
		"2090",
		"https://ci.example/acme/app/pipelines/results/2090",
		"https://bitbucket.org/acme/app/pull-requests/7",
		"https://bitbucket.org/acme/app/pipelines/results/latest",
		"https://bitbucket.org/acme/app/addon/pipelines/home#!/branches",
		"https://bitbucket.org/acme/app/pipelines/results/0",
	} {
		if got, ok := ParsePipelineResultURL(raw); ok {
			t.Errorf("ParsePipelineResultURL(%q) = %+v, want no match", raw, got)
		}
	}
}
