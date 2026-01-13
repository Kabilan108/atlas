package git

import (
	"fmt"
	"os/exec"
)

func FetchAndCheckout(remote, branch string) error {
	fetchCmd := exec.Command("git", "fetch", remote, branch)
	if output, err := fetchCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git fetch failed: %s", output)
	}

	checkoutCmd := exec.Command("git", "checkout", branch)
	if output, err := checkoutCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout failed: %s", output)
	}

	return nil
}
