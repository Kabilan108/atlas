package pipeline

import "time"

// FastPolls makes pipeline_run's wait poll every millisecond until the test
// ends.
func FastPolls(t interface{ Cleanup(func()) }) {
	initial, maximum := pollInitial, pollMax
	pollInitial, pollMax = time.Millisecond, time.Millisecond
	t.Cleanup(func() { pollInitial, pollMax = initial, maximum })
}
