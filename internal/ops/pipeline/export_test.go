package pipeline

import "time"

// FastPolls makes pipeline_run's wait poll every millisecond until the test
// ends.
func FastPolls(t interface{ Cleanup(func()) }) {
	initial, maximum := pollInitial, pollMax
	pollInitial, pollMax = time.Millisecond, time.Millisecond
	t.Cleanup(func() { pollInitial, pollMax = initial, maximum })
}

// SetPolls sets pipeline_run's poll intervals until the test ends.
func SetPolls(t interface{ Cleanup(func()) }, initial, maximum time.Duration) {
	oldInitial, oldMaximum := pollInitial, pollMax
	pollInitial, pollMax = initial, maximum
	t.Cleanup(func() { pollInitial, pollMax = oldInitial, oldMaximum })
}
