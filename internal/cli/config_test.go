package cli

import (
	"testing"

	"github.com/kabilan108/atlas/internal/bitbucket"
)

func TestFormatVerifiedUser(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		user bitbucket.User
		want string
	}{
		{
			name: "display name and username",
			user: bitbucket.User{DisplayName: "Jane Doe", Username: "jdoe"},
			want: "Authenticated as Jane Doe (jdoe)\n",
		},
		{
			name: "display name only",
			user: bitbucket.User{DisplayName: "Jane Doe"},
			want: "Authenticated as Jane Doe\n",
		},
		{
			name: "nickname only fallback",
			user: bitbucket.User{Nickname: "Jane Doe"},
			want: "Authenticated as Jane Doe\n",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := formatVerifiedUser(tt.user); got != tt.want {
				t.Fatalf("formatVerifiedUser() = %q, want %q", got, tt.want)
			}
		})
	}
}
