---
pr: 42
workspace: 'acme'
repo: 'widgets'
title: 'Fix login: handle ''empty'' users'
state: 'OPEN'
draft: false
author:
  name: 'ada'
  mention: '@ada'
source_branch: 'feature/login'
destination_branch: 'main'
updated_at: '2025-03-05T16:30:00Z'
bitbucket_url: 'https://bitbucket.org/acme/widgets/pull-requests/42'
comment_count: 5
unresolved_comment_count: 3
task_count: 2
reviewers:
  -
    name: 'grace'
    mention: '@grace'
    status: 'approved'
  -
    name: 'ken'
    mention: '@ken'
    status: 'pending'
  -
    name: 'linus'
    mention: '@linus'
    status: 'changes_requested'
---

# PR #42: Fix login: handle 'empty' users

## Review Summary

- Reviewers: 1 approved, 1 changes requested, 1 pending
- Comments: 5 total, 3 unresolved comments, 2 unresolved threads
- Files with feedback: 1
- Actionable: general — @linus: Please add tests for @{557058:ghost} before merging.
- Actionable: src/login.go:12 — @grace: Handle the error here.
- Tasks: 1 open, 1 resolved

## Description

Fixes login for @margaret and @grace.

- returns errEmpty
- keeps the session

```
@{557058:in-fence}
```

See `@{557058:inline-code}` for details.


## Comments

### Thread: General [OPEN]

Type: comment
ID: 103
Author: @linus
When: Mar 3, 2025 - 2025-03-03 08:00

Please add tests for @{557058:ghost} before merging. Otherwise fine.
---

### Thread: `README.md` [RESOLVED]

Type: comment
ID: 104
Author: @grace
When: Mar 3, 2025 - 2025-03-03 09:00
Status: RESOLVED

Typo in the heading.
---

### Thread: `src/login.go:12` [UNRESOLVED]

```diff
@@ -8,5 +8,7 @@ func Login(user string) error {
 		return errEmpty
 	}
+	token, err := issue(user)
+	if err != nil { return err }
 	return save(token)
 }
```

Type: comment
ID: 101
Author: @grace
When: Mar 2, 2025 - 2025-03-02 10:00
Status: UNRESOLVED

Handle the error here. Also loop in [@Margaret Hamilton](https://bitbucket.org/margaret).

Type: reply
ID: 102
Author: @ada (author)
When: Mar 2, 2025 - 2025-03-02 11:00
Status: UNRESOLVED

Done, thanks.

## Tasks

### Task #1 [OPEN]

Author: @linus
Thread: `src/login.go:12`

Add tests for the empty user
---

### Task #2 [RESOLVED]

Author: @grace

Fix typo flagged by @margaret
