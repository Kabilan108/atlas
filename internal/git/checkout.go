package git

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
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

func CurrentBranch() (string, error) {
	output, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("failed to determine current branch: %w", err)
	}
	branch := strings.TrimSpace(string(output))
	if branch == "" || branch == "HEAD" {
		return "", fmt.Errorf("current checkout is not on a branch")
	}
	return branch, nil
}

func Fetch(remote, refspec string) error {
	cmd := exec.Command("git", "fetch", remote, refspec)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git fetch failed: %s", output)
	}
	return nil
}

func CheckoutBranch(localBranch, startPoint string, force bool) error {
	args := []string{"checkout"}
	if force {
		args = append(args, "-B")
	} else {
		args = append(args, "-b")
	}
	args = append(args, localBranch, startPoint)
	cmd := exec.Command("git", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		if !force {
			cmd = exec.Command("git", "checkout", localBranch)
			if checkoutOutput, checkoutErr := cmd.CombinedOutput(); checkoutErr == nil {
				return nil
			} else {
				return fmt.Errorf("git checkout failed: %s", checkoutOutput)
			}
		}
		return fmt.Errorf("git checkout failed: %s", output)
	}
	return nil
}

func CheckoutDetached(startPoint string) error {
	cmd := exec.Command("git", "checkout", "--detach", startPoint)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout --detach failed: %s", output)
	}
	return nil
}

func UpdateSubmodules() error {
	cmd := exec.Command("git", "submodule", "update", "--init", "--recursive")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git submodule update failed: %s", output)
	}
	return nil
}

func RemoteExists(name string) bool {
	return exec.Command("git", "remote", "get-url", name).Run() == nil
}

func AddRemote(name, url string) error {
	cmd := exec.Command("git", "remote", "add", name, url)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git remote add failed: %s", output)
	}
	return nil
}

func RemoteBranchExists(remote, branch string) bool {
	ref := fmt.Sprintf("refs/remotes/%s/%s", remote, branch)
	return exec.Command("git", "show-ref", "--verify", "--quiet", ref).Run() == nil
}

func RefExists(ref string) bool {
	if strings.TrimSpace(ref) == "" {
		return false
	}
	return exec.Command("git", "rev-parse", "--verify", "--quiet", ref+"^{commit}").Run() == nil
}

func PushCurrentBranch(remote, branch string) error {
	refspec := fmt.Sprintf("HEAD:%s", branch)
	cmd := exec.Command("git", "push", "-u", remote, refspec)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git push failed: %w", err)
	}
	return nil
}

func PushBranch(remote, branch string) error {
	refspec := fmt.Sprintf("%s:%s", branch, branch)
	cmd := exec.Command("git", "push", "-u", remote, refspec)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git push failed: %w", err)
	}
	return nil
}

type CommitMessage struct {
	Subject string
	Body    string
}

func CommitMessages(base, head string) ([]CommitMessage, error) {
	rangeSpec := fmt.Sprintf("%s..%s", base, head)
	output, err := exec.Command("git", "log", "--format=%s%x00%b%x00%x1e", rangeSpec).Output()
	if err != nil {
		return nil, fmt.Errorf("failed to read commit messages: %w", err)
	}

	records := strings.Split(string(output), "\x1e")
	messages := make([]CommitMessage, 0, len(records))
	for _, record := range records {
		record = strings.Trim(record, "\n")
		if record == "" {
			continue
		}
		parts := strings.SplitN(record, "\x00", 3)
		if len(parts) < 2 {
			continue
		}
		messages = append(messages, CommitMessage{
			Subject: strings.TrimSpace(parts[0]),
			Body:    strings.TrimSpace(parts[1]),
		})
	}
	return messages, nil
}

func GitPager() string {
	if pager, err := exec.Command("git", "var", "GIT_PAGER").Output(); err == nil {
		if value := strings.TrimSpace(string(pager)); value != "" {
			return value
		}
	}
	for _, key := range []string{"GIT_PAGER", "PAGER"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	if _, err := exec.LookPath("less"); err == nil {
		return "less"
	}
	return ""
}
