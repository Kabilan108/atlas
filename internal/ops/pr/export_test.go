package pr

import "time"

// FastChecksPolls makes pr_checks' wait poll every millisecond until the
// test ends.
func FastChecksPolls(t interface{ Cleanup(func()) }) {
	initial, maximum := checksPollInitial, checksPollMax
	checksPollInitial, checksPollMax = time.Millisecond, time.Millisecond
	t.Cleanup(func() { checksPollInitial, checksPollMax = initial, maximum })
}

// SetChecksPolls makes pr_checks' wait poll every interval until the test
// ends.
func SetChecksPolls(t interface{ Cleanup(func()) }, interval time.Duration) {
	initial, maximum := checksPollInitial, checksPollMax
	checksPollInitial, checksPollMax = interval, interval
	t.Cleanup(func() { checksPollInitial, checksPollMax = initial, maximum })
}
