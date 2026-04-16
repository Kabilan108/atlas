package bitbucket

import "testing"

func TestUserHandleFallsBackFromUsernameToNicknameToDisplayName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		user User
		want string
	}{
		{
			name: "prefers username",
			user: User{Username: "jdoe", Nickname: "Jane Doe", DisplayName: "Jane Doe"},
			want: "jdoe",
		},
		{
			name: "falls back to nickname",
			user: User{Nickname: "Jane Doe", DisplayName: "Jane Doe"},
			want: "Jane Doe",
		},
		{
			name: "falls back to display name",
			user: User{DisplayName: "Jane Doe"},
			want: "Jane Doe",
		},
		{
			name: "falls back to account id",
			user: User{AccountID: "acct-123"},
			want: "acct-123",
		},
		{
			name: "falls back to uuid without braces",
			user: User{UUID: "{user-123}"},
			want: "user-123",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.user.Handle(); got != tt.want {
				t.Fatalf("Handle() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUserMatchesStableIdentifier(t *testing.T) {
	t.Parallel()

	user := User{
		Username:    "jdoe",
		UUID:        "{user-123}",
		Nickname:    "Jane Doe",
		DisplayName: "Jane Doe",
		AccountID:   "acct-123",
	}

	for _, candidate := range []string{
		"jdoe",
		"@jdoe",
		"acct-123",
		"user-123",
		"{user-123}",
	} {
		if !user.MatchesStableIdentifier(candidate) {
			t.Fatalf("MatchesStableIdentifier(%q) = false, want true", candidate)
		}
	}

	for _, candidate := range []string{"Jane Doe", "@Jane Doe"} {
		if user.MatchesStableIdentifier(candidate) {
			t.Fatalf("MatchesStableIdentifier(%q) = true, want false", candidate)
		}
	}
}

func TestUserSharesStableIdentity(t *testing.T) {
	t.Parallel()

	if !(User{AccountID: "acct-123"}).SharesStableIdentity(User{AccountID: "acct-123"}) {
		t.Fatal("SharesStableIdentity() = false, want true for matching account ids")
	}

	if (User{Nickname: "Jane Doe"}).SharesStableIdentity(User{DisplayName: "Jane Doe"}) {
		t.Fatal("SharesStableIdentity() = true, want false for display-only identities")
	}
}
