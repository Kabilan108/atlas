package pr

import "testing"

func TestNumberDiffHunks(t *testing.T) {
	diff := "diff --git a/a.txt b/a.txt\n" +
		"--- a/a.txt\n" +
		"+++ b/a.txt\n" +
		"@@ -1 +1 @@\n" +
		"-old\n" +
		"\\ No newline at end of file\n" +
		"+new\n" +
		"\\ No newline at end of file\n" +
		"diff --git a/b.txt b/b.txt\n" +
		"--- a/b.txt\n" +
		"+++ b/b.txt\n" +
		"@@ -98,2 +99,0 @@\n" +
		"--- looks like a header\n" +
		"-gone\n" +
		"--- a/c.txt\n"
	want := "diff --git a/a.txt b/a.txt\n" +
		"--- a/a.txt\n" +
		"+++ b/a.txt\n" +
		"@@ -1 +1 @@\n" +
		"  1     -old\n" +
		"        \\ No newline at end of file\n" +
		"      1 +new\n" +
		"        \\ No newline at end of file\n" +
		"diff --git a/b.txt b/b.txt\n" +
		"--- a/b.txt\n" +
		"+++ b/b.txt\n" +
		"@@ -98,2 +99,0 @@\n" +
		" 98     --- looks like a header\n" +
		" 99     -gone\n" +
		"--- a/c.txt\n"
	if got := string(numberDiff([]byte(diff))); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}
