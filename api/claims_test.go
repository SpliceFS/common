package api

import "testing"

func TestClaimsAllows(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		shares []string
		share  string
		want   bool
	}{
		{"listed", []string{"media", "docs"}, "docs", true},
		{"not listed", []string{"media"}, "docs", false},
		{"wildcard", []string{"*"}, "anything", true},
		{"wildcard among others", []string{"media", "*"}, "docs", true},
		{"none", nil, "media", false},
		{"empty share is not a wildcard", []string{"media"}, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := (Claims{Shares: tt.shares}).Allows(tt.share); got != tt.want {
				t.Errorf("Allows(%q) = %v, want %v", tt.share, got, tt.want)
			}
		})
	}
}
