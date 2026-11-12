package units

import (
	"math"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

func TestSizeUnmarshalText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want Size
	}{
		{"0", 0},
		{"4096", 4096},
		{"007", 7},
		{"0KiB", 0},
		{"1KiB", 1 << 10},
		{"5MiB", 5 << 20},
		{"1.5GiB", 3 << 29},
		{"0.5KiB", 512},
		{"2TiB", 2 << 40},
		{"8388607TiB", 8388607 << 40},
		{"9223372036854775806", math.MaxInt64 - 1},
		{"unlimited", UnlimitedSize},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			var got Size
			if err := got.UnmarshalText([]byte(tc.in)); err != nil {
				t.Fatalf("UnmarshalText(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("UnmarshalText(%q) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestSizeUnmarshalTextErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in      string
		wantErr string
	}{
		{"", "not a number"},
		{"-1", "not a number"},
		{"-1MiB", "not a number"},
		{"MiB", "not a number"},
		{"1.", "not a number"},
		{".5MiB", "not a number"},
		{"1.2.3MiB", "not a number"},
		{"1MiB2", "not a number"},
		{"5 MiB", `unknown unit " MiB"`},
		{"5KB", `unknown unit "KB"`},
		{"5MB", `unknown unit "MB"`},
		{"5k", `unknown unit "k"`},
		{"5mib", `unknown unit "mib"`},
		{"Unlimited", "not a number"},
		{"1.5", "whole number"},
		{"0.1KiB", "whole number"},
		{"9223372036854775807", "too large"},
		{"9223372036854775808", "too large"},
		{"8388608TiB", "too large"},
		{"99999999999999999999999999999999999999TiB", "too large"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			s := Size(42)
			err := s.UnmarshalText([]byte(tc.in))
			if err == nil {
				t.Fatalf("UnmarshalText(%q) succeeded with %d", tc.in, s)
			}
			for _, want := range []string{tc.wantErr, `"` + tc.in + `"`, "KiB, MiB, GiB or TiB", "unlimited"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
			if s != 42 {
				t.Errorf("value changed to %d on error", s)
			}
		})
	}
}

func TestSizeString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   Size
		want string
	}{
		{0, "0"},
		{1, "1"},
		{1000, "1000"},
		{1 << 10, "1KiB"},
		{1536 << 10, "1536KiB"},
		{5 << 20, "5MiB"},
		{1<<20 + 1, "1048577"},
		{3 << 30, "3GiB"},
		{1 << 40, "1TiB"},
		{2048 << 30, "2TiB"},
		{UnlimitedSize, "unlimited"},
	}
	for _, tc := range tests {
		if got := tc.in.String(); got != tc.want {
			t.Errorf("Size(%d).String() = %q, want %q", int64(tc.in), got, tc.want)
		}
	}
}

func TestSizeUnlimited(t *testing.T) {
	t.Parallel()
	if !UnlimitedSize.Unlimited() {
		t.Error("UnlimitedSize.Unlimited() = false")
	}
	if (Size(5)).Unlimited() {
		t.Error("Size(5).Unlimited() = true")
	}
}

func TestSizeRoundTrip(t *testing.T) {
	t.Parallel()
	for _, s := range []Size{0, 1, 1023, 1024, 1<<20 + 1, 5 << 20, 7 << 40, UnlimitedSize} {
		text, err := s.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText(%d): %v", int64(s), err)
		}
		var got Size
		if err := got.UnmarshalText(text); err != nil {
			t.Fatalf("UnmarshalText(%q): %v", text, err)
		}
		if got != s {
			t.Errorf("round trip of %d through %q gave %d", int64(s), text, int64(got))
		}
	}
}

func TestDurationUnmarshalText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want time.Duration
	}{
		{"0", 0},
		{"0s", 0},
		{"1s", time.Second},
		{"30s", 30 * time.Second},
		{"1.5s", 1500 * time.Millisecond},
		{"0.000000001s", time.Nanosecond},
		{"90m", 90 * time.Minute},
		{"0.5h", 30 * time.Minute},
		{"2h", 2 * time.Hour},
		{"1d", 24 * time.Hour},
		{"30d", 30 * 24 * time.Hour},
		{"1.5d", 36 * time.Hour},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			var got Duration
			if err := got.UnmarshalText([]byte(tc.in)); err != nil {
				t.Fatalf("UnmarshalText(%q): %v", tc.in, err)
			}
			if got.Std() != tc.want {
				t.Errorf("UnmarshalText(%q) = %v, want %v", tc.in, got.Std(), tc.want)
			}
		})
	}
	var u Duration
	if err := u.UnmarshalText([]byte("unlimited")); err != nil {
		t.Fatalf("UnmarshalText(unlimited): %v", err)
	}
	if !u.Unlimited() || u != UnlimitedDuration {
		t.Errorf("unlimited parsed as %d", int64(u))
	}
}

func TestDurationUnmarshalTextErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in      string
		wantErr string
	}{
		{"", "not a number"},
		{"-1s", "not a number"},
		{"-0", "not a number"},
		{"1h30m", "not a number"},
		{"5", "missing unit"},
		{"00", "missing unit"},
		{"0.0", "missing unit"},
		{"5ms", `unknown unit "ms"`},
		{"5w", `unknown unit "w"`},
		{"5S", `unknown unit "S"`},
		{"5 s", `unknown unit " s"`},
		{"s", "not a number"},
		{"0.0000000001s", "whole number"},
		{"9223372036854775807s", "too large"},
		{"106752d", "too large"},
		{"99999999999999999999999999999999999999d", "too large"},
		{"Unlimited", "not a number"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			d := Duration(42)
			err := d.UnmarshalText([]byte(tc.in))
			if err == nil {
				t.Fatalf("UnmarshalText(%q) succeeded with %d", tc.in, int64(d))
			}
			for _, want := range []string{tc.wantErr, `"` + tc.in + `"`, "s, m, h or d", "unlimited"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
			if d != 42 {
				t.Errorf("value changed to %d on error", int64(d))
			}
		})
	}
}

func TestDurationString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   time.Duration
		want string
	}{
		{0, "0"},
		{time.Second, "1s"},
		{90 * time.Second, "90s"},
		{time.Minute, "1m"},
		{90 * time.Minute, "90m"},
		{2 * time.Hour, "2h"},
		{36 * time.Hour, "36h"},
		{24 * time.Hour, "1d"},
		{30 * 24 * time.Hour, "30d"},
		{1500 * time.Millisecond, "1.5s"},
		{time.Second + time.Nanosecond, "1.000000001s"},
		{time.Millisecond, "0.001s"},
		{-time.Second, "-1s"},
	}
	for _, tc := range tests {
		if got := Duration(tc.in).String(); got != tc.want {
			t.Errorf("Duration(%d).String() = %q, want %q", int64(tc.in), got, tc.want)
		}
	}
	if got := UnlimitedDuration.String(); got != "unlimited" {
		t.Errorf("UnlimitedDuration.String() = %q", got)
	}
	if (Duration(time.Second)).Unlimited() {
		t.Error("1s is unlimited")
	}
}

func TestDurationRoundTrip(t *testing.T) {
	t.Parallel()
	values := []time.Duration{0, time.Nanosecond, 1500 * time.Millisecond, time.Second, 90 * time.Minute, 36 * time.Hour, 48 * time.Hour}
	for _, v := range values {
		d := Duration(v)
		text, err := d.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText(%v): %v", v, err)
		}
		var got Duration
		if err := got.UnmarshalText(text); err != nil {
			t.Fatalf("UnmarshalText(%q): %v", text, err)
		}
		if got != d {
			t.Errorf("round trip of %v through %q gave %v", v, text, got.Std())
		}
	}
	text, _ := UnlimitedDuration.MarshalText()
	var got Duration
	if err := got.UnmarshalText(text); err != nil || got != UnlimitedDuration {
		t.Errorf("round trip of unlimited gave %d, %v", int64(got), err)
	}
}

func TestYAML(t *testing.T) {
	t.Parallel()
	var doc struct {
		Cache   Size     `yaml:"cache"`
		Plain   Size     `yaml:"plain"`
		Never   Size     `yaml:"never"`
		Age     Duration `yaml:"age"`
		Zero    Duration `yaml:"zero"`
		Forever Duration `yaml:"forever"`
	}
	in := "cache: 5MiB\nplain: 4096\nnever: unlimited\nage: 30d\nzero: 0\nforever: unlimited\n"
	if err := yaml.Unmarshal([]byte(in), &doc); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if doc.Cache != 5<<20 || doc.Plain != 4096 || !doc.Never.Unlimited() {
		t.Errorf("sizes decoded as %v %v %v", doc.Cache, doc.Plain, doc.Never)
	}
	if doc.Age.Std() != 30*24*time.Hour || doc.Zero != 0 || !doc.Forever.Unlimited() {
		t.Errorf("durations decoded as %v %v %v", doc.Age, doc.Zero, doc.Forever)
	}
	out, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(out), "cache: 5MiB") || !strings.Contains(string(out), "age: 30d") {
		t.Errorf("Marshal output:\n%s", out)
	}
	if err := yaml.Unmarshal([]byte("cache: 5MB\n"), &doc); err == nil {
		t.Error("Unmarshal accepted 5MB")
	}
}

func FuzzSizeUnmarshalText(f *testing.F) {
	for _, s := range []string{
		"0", "4096", "007", "0KiB", "1KiB", "5MiB", "1.5GiB", "0.5KiB", "2TiB",
		"8388607TiB", "9223372036854775806", "unlimited",
		"", "-1", "MiB", "1.", ".5MiB", "1.2.3MiB", "1MiB2", "5 MiB", "5KB", "5mib",
		"Unlimited", "1.5", "0.1KiB", "9223372036854775807", "8388608TiB",
		"99999999999999999999999999999999999999TiB",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		s := Size(42)
		err := s.UnmarshalText([]byte(in))
		if err != nil {
			// A failed parse must leave the target alone so callers never see a half value.
			if s != 42 {
				t.Fatalf("value changed to %d on error for %q", int64(s), in)
			}
			return
		}
		// A size is a byte count, so it can never be negative or wrap around.
		if s < 0 {
			t.Fatalf("%q parsed to negative size %d", in, int64(s))
		}
		// Only the word "unlimited" may produce the reserved maximum value.
		if s.Unlimited() != (in == unlimitedWord) {
			t.Fatalf("%q parsed to %d, unlimited mismatch", in, int64(s))
		}
		// What a size writes must read back as the same size, or saved files would drift.
		text, err := s.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText(%d): %v", int64(s), err)
		}
		var back Size
		if err := back.UnmarshalText(text); err != nil {
			t.Fatalf("re-parse of %q (from %q): %v", text, in, err)
		}
		if back != s {
			t.Fatalf("round trip of %q via %q gave %d, want %d", in, text, int64(back), int64(s))
		}
	})
}

func FuzzDurationUnmarshalText(f *testing.F) {
	for _, s := range []string{
		"0", "0s", "1s", "30s", "1.5s", "0.000000001s", "90m", "0.5h", "2h", "1d", "30d",
		"1.5d", "unlimited",
		"", "-1s", "-0", "1h30m", "5", "00", "0.0", "5ms", "5w", "5S", "5 s", "s",
		"0.0000000001s", "9223372036854775807s", "106752d", "106751d",
		"99999999999999999999999999999999999999d", "Unlimited",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		d := Duration(42)
		err := d.UnmarshalText([]byte(in))
		if err != nil {
			// A failed parse must leave the target alone so callers never see a half value.
			if d != 42 {
				t.Fatalf("value changed to %d on error for %q", int64(d), in)
			}
			return
		}
		// A configured duration cannot be negative, which would mean an overflow wrapped.
		if d < 0 {
			t.Fatalf("%q parsed to negative duration %d", in, int64(d))
		}
		// Only the word "unlimited" may produce the reserved maximum value.
		if d.Unlimited() != (in == unlimitedWord) {
			t.Fatalf("%q parsed to %d, unlimited mismatch", in, int64(d))
		}
		// What a duration writes must read back as the same duration, or saved files drift.
		text, err := d.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText(%d): %v", int64(d), err)
		}
		var back Duration
		if err := back.UnmarshalText(text); err != nil {
			t.Fatalf("re-parse of %q (from %q): %v", text, in, err)
		}
		if back != d {
			t.Fatalf("round trip of %q via %q gave %d, want %d", in, text, int64(back), int64(d))
		}
	})
}

// FuzzDurationString covers String for every value, including negative ones that
// UnmarshalText never produces.
func FuzzDurationString(f *testing.F) {
	for _, v := range []int64{
		0, 1, 1500000000, 1000000001, -1, -1000000000, 86400000000000, math.MaxInt64,
		math.MaxInt64 - 1, math.MinInt64,
	} {
		f.Add(v)
	}
	f.Fuzz(func(t *testing.T, v int64) {
		d := Duration(v)
		text := d.String()
		// A rendering must never be empty, or a written file would hold a blank value.
		if text == "" {
			t.Fatalf("Duration(%d).String() is empty", v)
		}
		if d < 0 {
			return
		}
		// Every non-negative duration must survive being written and read back.
		var back Duration
		if err := back.UnmarshalText([]byte(text)); err != nil {
			t.Fatalf("Duration(%d) rendered as %q, which does not parse: %v", v, text, err)
		}
		if back != d {
			t.Fatalf("Duration(%d) rendered as %q, which parses to %d", v, text, int64(back))
		}
	})
}

// FuzzSizeString covers String for every value, including negative ones that
// UnmarshalText never produces.
func FuzzSizeString(f *testing.F) {
	for _, v := range []int64{
		0, 1, 1023, 1024, 1<<20 + 1, 5 << 20, 7 << 40, -1, math.MaxInt64, math.MinInt64,
	} {
		f.Add(v)
	}
	f.Fuzz(func(t *testing.T, v int64) {
		s := Size(v)
		text := s.String()
		// A rendering must never be empty, or a written file would hold a blank value.
		if text == "" {
			t.Fatalf("Size(%d).String() is empty", v)
		}
		if s < 0 {
			return
		}
		// Every non-negative size must survive being written and read back.
		var back Size
		if err := back.UnmarshalText([]byte(text)); err != nil {
			t.Fatalf("Size(%d) rendered as %q, which does not parse: %v", v, text, err)
		}
		if back != s {
			t.Fatalf("Size(%d) rendered as %q, which parses to %d", v, text, int64(back))
		}
	})
}

func FuzzYAML(f *testing.F) {
	for _, s := range []string{
		"cache: 5MiB\nage: 30d\n",
		"cache: 4096\nage: 0\n",
		"cache: unlimited\nage: unlimited\n",
		"cache: 5MB\n",
		"cache: [1, 2]\n",
		"cache: {a: b}\nage: ~\n",
		"cache: 1.5\nage: 1.5\n",
		"cache: -1\n",
		"age: 1h30m\n",
		"cache: &a 5MiB\nage: *a\n",
		"",
		": :\n\t",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		var doc struct {
			Cache Size     `yaml:"cache"`
			Age   Duration `yaml:"age"`
		}
		// Untrusted config text must produce an error, never a panic.
		if err := yaml.Unmarshal([]byte(in), &doc); err != nil {
			return
		}
		// Decoding through YAML must hold the same bounds as decoding plain text.
		if doc.Cache < 0 || doc.Age < 0 {
			t.Fatalf("%q decoded to negative values %d, %d", in, int64(doc.Cache), int64(doc.Age))
		}
		// A decoded document must encode and decode again unchanged.
		out, err := yaml.Marshal(doc)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		back := doc
		back.Cache, back.Age = 1, 1
		if err := yaml.Unmarshal(out, &back); err != nil {
			t.Fatalf("re-decode of %q: %v", out, err)
		}
		if back != doc {
			t.Fatalf("round trip of %q gave %+v, want %+v", in, back, doc)
		}
	})
}
