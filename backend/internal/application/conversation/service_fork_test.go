package conversation

import "testing"

func TestNormalizeForkedMessageStatus(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "keeps success", in: "success", want: "success"},
		{name: "keeps interrupted", in: "interrupted", want: "interrupted"},
		{name: "keeps error", in: "error", want: "error"},
		{name: "keeps blocked", in: "blocked", want: "blocked"},
		{name: "maps pending to interrupted", in: "pending", want: "interrupted"},
		{name: "maps pending with spaces to interrupted", in: "  pending  ", want: "interrupted"},
		{name: "falls back to success for empty", in: "", want: "success"},
		{name: "falls back to success for blank", in: "   ", want: "success"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeForkedMessageStatus(tc.in); got != tc.want {
				t.Fatalf("normalizeForkedMessageStatus(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
