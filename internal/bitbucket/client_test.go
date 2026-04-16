package bitbucket

import "testing"

func TestHasReviewerMatchesStableIdentifiersOnly(t *testing.T) {
	t.Parallel()

	pr := PullRequest{
		Reviewers: []User{
			{
				Username:    "jdoe",
				Nickname:    "Jane Doe",
				DisplayName: "Jane Doe",
				AccountID:   "acct-123",
			},
		},
		Participants: []Participant{
			{
				Role: "REVIEWER",
				User: User{
					UUID:        "{reviewer-123}",
					DisplayName: "Jane Doe",
				},
			},
		},
	}

	for _, reviewer := range []string{"jdoe", "@jdoe", "acct-123", "reviewer-123"} {
		if !hasReviewer(pr, reviewer) {
			t.Fatalf("hasReviewer(%q) = false, want true", reviewer)
		}
	}

	for _, reviewer := range []string{"Jane Doe", "@Jane Doe"} {
		if hasReviewer(pr, reviewer) {
			t.Fatalf("hasReviewer(%q) = true, want false", reviewer)
		}
	}
}
