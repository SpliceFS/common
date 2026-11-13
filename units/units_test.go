package units

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

// sentinel is the value a target holds before a parse. A failed parse must leave it
// unchanged, so a changed value shows that the parse wrote a half result.
const sentinel = 42

func TestSizeUnmarshalText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  Size
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
	for _, testCase := range tests {
		t.Run(testCase.input, func(t *testing.T) {
			t.Parallel()
			var got Size
			if err := got.UnmarshalText([]byte(testCase.input)); err != nil {
				t.Fatalf("UnmarshalText(%q): %v", testCase.input, err)
			}
			if got != testCase.want {
				t.Errorf("UnmarshalText(%q) = %d, want %d", testCase.input, got, testCase.want)
			}
		})
	}
}

func TestSizeUnmarshalTextErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input   string
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
	for _, testCase := range tests {
		t.Run(testCase.input, func(t *testing.T) {
			t.Parallel()
			size := Size(sentinel)
			err := size.UnmarshalText([]byte(testCase.input))
			if err == nil {
				t.Fatalf("UnmarshalText(%q) succeeded with %d", testCase.input, size)
			}
			for _, want := range []string{testCase.wantErr, `"` + testCase.input + `"`, "KiB, MiB, GiB or TiB", "unlimited"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
			if size != sentinel {
				t.Errorf("value changed to %d on error", size)
			}
		})
	}
}

func TestSizeString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input Size
		want  string
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
	for _, testCase := range tests {
		if got := testCase.input.String(); got != testCase.want {
			t.Errorf("Size(%d).String() = %q, want %q", int64(testCase.input), got, testCase.want)
		}
	}
}

func TestSizeUnlimited(t *testing.T) {
	t.Parallel()
	if !UnlimitedSize.Unlimited() {
		t.Error("UnlimitedSize.Unlimited() = false")
	}
	if Size(5).Unlimited() {
		t.Error("Size(5).Unlimited() = true")
	}
}

func TestSizeRoundTrip(t *testing.T) {
	t.Parallel()
	for _, size := range []Size{0, 1, 1023, 1024, 1<<20 + 1, 5 << 20, 7 << 40, UnlimitedSize} {
		text, err := size.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText(%d): %v", int64(size), err)
		}
		var got Size
		if err := got.UnmarshalText(text); err != nil {
			t.Fatalf("UnmarshalText(%q): %v", text, err)
		}
		if got != size {
			t.Errorf("round trip of %d through %q gave %d", int64(size), text, int64(got))
		}
	}
}

func TestDurationUnmarshalText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  time.Duration
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
	for _, testCase := range tests {
		t.Run(testCase.input, func(t *testing.T) {
			t.Parallel()
			var got Duration
			if err := got.UnmarshalText([]byte(testCase.input)); err != nil {
				t.Fatalf("UnmarshalText(%q): %v", testCase.input, err)
			}
			if got.Std() != testCase.want {
				t.Errorf("UnmarshalText(%q) = %v, want %v", testCase.input, got.Std(), testCase.want)
			}
		})
	}
	var unlimited Duration
	if err := unlimited.UnmarshalText([]byte("unlimited")); err != nil {
		t.Fatalf("UnmarshalText(unlimited): %v", err)
	}
	if !unlimited.Unlimited() || unlimited != UnlimitedDuration {
		t.Errorf("unlimited parsed as %d", int64(unlimited))
	}
}

func TestDurationUnmarshalTextErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input   string
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
	for _, testCase := range tests {
		t.Run(testCase.input, func(t *testing.T) {
			t.Parallel()
			duration := Duration(sentinel)
			err := duration.UnmarshalText([]byte(testCase.input))
			if err == nil {
				t.Fatalf("UnmarshalText(%q) succeeded with %d", testCase.input, int64(duration))
			}
			for _, want := range []string{testCase.wantErr, `"` + testCase.input + `"`, "s, m, h or d", "unlimited"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
			if duration != sentinel {
				t.Errorf("value changed to %d on error", int64(duration))
			}
		})
	}
}

func TestDurationString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input time.Duration
		want  string
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
	for _, testCase := range tests {
		if got := Duration(testCase.input).String(); got != testCase.want {
			t.Errorf("Duration(%d).String() = %q, want %q", int64(testCase.input), got, testCase.want)
		}
	}
	if got := UnlimitedDuration.String(); got != "unlimited" {
		t.Errorf("UnlimitedDuration.String() = %q", got)
	}
	if Duration(time.Second).Unlimited() {
		t.Error("1s is unlimited")
	}
}

func TestDurationRoundTrip(t *testing.T) {
	t.Parallel()
	values := []time.Duration{
		0, time.Nanosecond, 1500 * time.Millisecond, time.Second, 90 * time.Minute, 36 * time.Hour, 48 * time.Hour,
	}
	for _, value := range values {
		duration := Duration(value)
		text, err := duration.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText(%v): %v", value, err)
		}
		var got Duration
		if err := got.UnmarshalText(text); err != nil {
			t.Fatalf("UnmarshalText(%q): %v", text, err)
		}
		if got != duration {
			t.Errorf("round trip of %v through %q gave %v", value, text, got.Std())
		}
	}
	text, err := UnlimitedDuration.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText(unlimited): %v", err)
	}
	var got Duration
	if err := got.UnmarshalText(text); err != nil || got != UnlimitedDuration {
		t.Errorf("round trip of unlimited gave %d, %v", int64(got), err)
	}
}

// yamlDocument is a configuration file that holds every kind of value of the package.
type yamlDocument struct {
	Cache   Size     `yaml:"cache"`
	Plain   Size     `yaml:"plain"`
	Never   Size     `yaml:"never"`
	Age     Duration `yaml:"age"`
	Zero    Duration `yaml:"zero"`
	Forever Duration `yaml:"forever"`
}

const yamlInput = "cache: 5MiB\nplain: 4096\nnever: unlimited\nage: 30d\nzero: 0\nforever: unlimited\n"

func TestYAMLDecode(t *testing.T) {
	t.Parallel()
	var doc yamlDocument
	if err := yaml.Unmarshal([]byte(yamlInput), &doc); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if doc.Cache != 5<<20 || doc.Plain != 4096 || !doc.Never.Unlimited() {
		t.Errorf("sizes decoded as %v %v %v", doc.Cache, doc.Plain, doc.Never)
	}
	if doc.Age.Std() != 30*24*time.Hour || doc.Zero != 0 || !doc.Forever.Unlimited() {
		t.Errorf("durations decoded as %v %v %v", doc.Age, doc.Zero, doc.Forever)
	}
}

func TestYAMLEncode(t *testing.T) {
	t.Parallel()
	var doc yamlDocument
	if err := yaml.Unmarshal([]byte(yamlInput), &doc); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	out, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, want := range []string{"cache: 5MiB", "age: 30d"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("Marshal output does not contain %q:\n%s", want, out)
		}
	}
}

func TestYAMLRejectsUnknownUnit(t *testing.T) {
	t.Parallel()
	var doc yamlDocument
	if err := yaml.Unmarshal([]byte("cache: 5MB\n"), &doc); err == nil {
		t.Error("Unmarshal accepted 5MB")
	}
}

// TestParseErrorsAreDeclared checks that every parse failure can be recognized with
// errors.Is, and that the message still names the offending value.
func TestParseErrorsAreDeclared(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  error
	}{
		{"1x", ErrUnknownUnit},
		{"x", ErrNoNumber},
		{"1.5", ErrNotWhole},
		{"9223372036854775807", ErrTooLarge},
	}
	for _, testCase := range tests {
		t.Run(testCase.input, func(t *testing.T) {
			t.Parallel()
			var size Size
			if err := size.UnmarshalText([]byte(testCase.input)); !errors.Is(err, testCase.want) {
				t.Errorf("Size %q: error %v is not %v", testCase.input, err, testCase.want)
			}
		})
	}
	var duration Duration
	if err := duration.UnmarshalText([]byte("1x")); !errors.Is(err, ErrUnknownUnit) {
		t.Errorf("Duration 1x: error %v is not %v", err, ErrUnknownUnit)
	}
	if err := duration.UnmarshalText([]byte("5")); !errors.Is(err, ErrNoUnit) {
		t.Errorf("Duration 5: error %v is not %v", err, ErrNoUnit)
	}
}

// requireUnchangedOnError fails when a failed parse changed its target, which would
// let callers see a half value.
func requireUnchangedOnError(t *testing.T, input string, got, before int64) {
	t.Helper()
	if got != before {
		t.Fatalf("value changed to %d on error for %q", got, input)
	}
}

// requireSizeRoundTrip fails unless what a size writes reads back as the same size, or
// saved files would drift.
func requireSizeRoundTrip(t *testing.T, input string, size Size) {
	t.Helper()
	text, err := size.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText(%d): %v", int64(size), err)
	}
	var back Size
	if err := back.UnmarshalText(text); err != nil {
		t.Fatalf("re-parse of %q (from %q): %v", text, input, err)
	}
	if back != size {
		t.Fatalf("round trip of %q via %q gave %d, want %d", input, text, int64(back), int64(size))
	}
}

// requireDurationRoundTrip is requireSizeRoundTrip for durations.
func requireDurationRoundTrip(t *testing.T, input string, duration Duration) {
	t.Helper()
	text, err := duration.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText(%d): %v", int64(duration), err)
	}
	var back Duration
	if err := back.UnmarshalText(text); err != nil {
		t.Fatalf("re-parse of %q (from %q): %v", text, input, err)
	}
	if back != duration {
		t.Fatalf("round trip of %q via %q gave %d, want %d", input, text, int64(back), int64(duration))
	}
}

// requireValidParsedSize checks the properties of a size that parsed without error.
func requireValidParsedSize(t *testing.T, input string, size Size) {
	t.Helper()
	// A size is a byte count, so it can never be negative or wrap around.
	if size < 0 {
		t.Fatalf("%q parsed to negative size %d", input, int64(size))
	}
	// Only the word "unlimited" may produce the reserved maximum value.
	if size.Unlimited() != (input == unlimitedWord) {
		t.Fatalf("%q parsed to %d, unlimited mismatch", input, int64(size))
	}
	requireSizeRoundTrip(t, input, size)
}

// requireValidParsedDuration checks the properties of a duration that parsed without error.
func requireValidParsedDuration(t *testing.T, input string, duration Duration) {
	t.Helper()
	// A configured duration cannot be negative, which would mean an overflow wrapped.
	if duration < 0 {
		t.Fatalf("%q parsed to negative duration %d", input, int64(duration))
	}
	// Only the word "unlimited" may produce the reserved maximum value.
	if duration.Unlimited() != (input == unlimitedWord) {
		t.Fatalf("%q parsed to %d, unlimited mismatch", input, int64(duration))
	}
	requireDurationRoundTrip(t, input, duration)
}

func FuzzSizeUnmarshalText(f *testing.F) {
	for _, seed := range []string{
		"0", "4096", "007", "0KiB", "1KiB", "5MiB", "1.5GiB", "0.5KiB", "2TiB",
		"8388607TiB", "9223372036854775806", "unlimited",
		"", "-1", "MiB", "1.", ".5MiB", "1.2.3MiB", "1MiB2", "5 MiB", "5KB", "5mib",
		"Unlimited", "1.5", "0.1KiB", "9223372036854775807", "8388608TiB",
		"99999999999999999999999999999999999999TiB",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		size := Size(sentinel)
		if err := size.UnmarshalText([]byte(input)); err != nil {
			requireUnchangedOnError(t, input, int64(size), sentinel)
			return
		}
		requireValidParsedSize(t, input, size)
	})
}

func FuzzDurationUnmarshalText(f *testing.F) {
	for _, seed := range []string{
		"0", "0s", "1s", "30s", "1.5s", "0.000000001s", "90m", "0.5h", "2h", "1d", "30d",
		"1.5d", "unlimited",
		"", "-1s", "-0", "1h30m", "5", "00", "0.0", "5ms", "5w", "5S", "5 s", "s",
		"0.0000000001s", "9223372036854775807s", "106752d", "106751d",
		"99999999999999999999999999999999999999d", "Unlimited",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		duration := Duration(sentinel)
		if err := duration.UnmarshalText([]byte(input)); err != nil {
			requireUnchangedOnError(t, input, int64(duration), sentinel)
			return
		}
		requireValidParsedDuration(t, input, duration)
	})
}

// FuzzDurationString covers String for every value, including negative ones that
// UnmarshalText never produces.
func FuzzDurationString(f *testing.F) {
	for _, value := range []int64{
		0, 1, 1500000000, 1000000001, -1, -1000000000, 86400000000000, math.MaxInt64,
		math.MaxInt64 - 1, math.MinInt64,
	} {
		f.Add(value)
	}
	f.Fuzz(func(t *testing.T, value int64) {
		duration := Duration(value)
		text := duration.String()
		// A rendering must never be empty, or a written file would hold a blank value.
		if text == "" {
			t.Fatalf("Duration(%d).String() is empty", value)
		}
		if duration < 0 {
			return
		}
		// Every non-negative duration must survive being written and read back.
		var back Duration
		if err := back.UnmarshalText([]byte(text)); err != nil {
			t.Fatalf("Duration(%d) rendered as %q, which does not parse: %v", value, text, err)
		}
		if back != duration {
			t.Fatalf("Duration(%d) rendered as %q, which parses to %d", value, text, int64(back))
		}
	})
}

// FuzzSizeString covers String for every value, including negative ones that
// UnmarshalText never produces.
func FuzzSizeString(f *testing.F) {
	for _, value := range []int64{
		0, 1, 1023, 1024, 1<<20 + 1, 5 << 20, 7 << 40, -1, math.MaxInt64, math.MinInt64,
	} {
		f.Add(value)
	}
	f.Fuzz(func(t *testing.T, value int64) {
		size := Size(value)
		text := size.String()
		// A rendering must never be empty, or a written file would hold a blank value.
		if text == "" {
			t.Fatalf("Size(%d).String() is empty", value)
		}
		if size < 0 {
			return
		}
		// Every non-negative size must survive being written and read back.
		var back Size
		if err := back.UnmarshalText([]byte(text)); err != nil {
			t.Fatalf("Size(%d) rendered as %q, which does not parse: %v", value, text, err)
		}
		if back != size {
			t.Fatalf("Size(%d) rendered as %q, which parses to %d", value, text, int64(back))
		}
	})
}

func FuzzYAML(f *testing.F) {
	for _, seed := range []string{
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
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		var doc struct {
			Cache Size     `yaml:"cache"`
			Age   Duration `yaml:"age"`
		}
		// Untrusted config text must produce an error, never a panic.
		if err := yaml.Unmarshal([]byte(input), &doc); err != nil {
			return
		}
		// Decoding through YAML must hold the same bounds as decoding plain text.
		if doc.Cache < 0 || doc.Age < 0 {
			t.Fatalf("%q decoded to negative values %d, %d", input, int64(doc.Cache), int64(doc.Age))
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
			t.Fatalf("round trip of %q gave %+v, want %+v", input, back, doc)
		}
	})
}
