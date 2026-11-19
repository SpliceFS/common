package api

import (
	"testing"
	"time"
)

func TestETag(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		size  int64
		mtime time.Time
		want  string
	}{
		{"whole seconds", 10, time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC), `"a-6ae729c0-0"`},
		{"nanoseconds", 4096, time.Date(2026, 11, 1, 12, 0, 1, 5, time.UTC), `"1000-6ae729c1-5"`},
		{"empty file at the epoch", 0, time.Unix(0, 0), `"0-0-0"`},
	}
	for _, tt := range tests {
		if got := ETag(tt.size, tt.mtime); got != tt.want {
			t.Errorf("%s: ETag = %s, want %s", tt.name, got, tt.want)
		}
	}
}
