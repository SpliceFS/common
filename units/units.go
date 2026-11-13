// Package units defines the size and duration types used in the YAML
// configuration files of both the server and the client.
//
// Sizes take the binary suffixes KiB, MiB, GiB and TiB. Durations take one of
// the suffixes s, m, h and d (24 hours). Both accept the word "unlimited".
package units

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// quantityPattern splits a value into integer digits, fraction digits and a
// unit. The unit is whatever follows the number and is checked separately.
var quantityPattern = regexp.MustCompile(`^(\d+)(?:\.(\d+))?([^0-9.]*)$`)

const unlimitedWord = "unlimited"

// decimalBase is the base of the digits in a configuration value.
const decimalBase = 10

// Errors reported when a size or duration cannot be parsed. UnmarshalText wraps
// them with the offending value, so callers test for them with errors.Is.
var (
	ErrNoNumber    = errors.New("not a number followed by a unit")
	ErrNoUnit      = errors.New("missing unit")
	ErrUnknownUnit = errors.New("unknown unit")
	ErrNotWhole    = errors.New("does not come to a whole number of base units")
	ErrTooLarge    = errors.New("too large")
)

// parseScaled parses a non-negative integer or decimal number followed by a
// unit. multiplier maps a unit to its size in base units. The result must be a
// whole number of base units and must be smaller than math.MaxInt64, which is
// reserved for "unlimited".
func parseScaled(text string, multiplier func(unit string) (int64, bool)) (int64, error) {
	match := quantityPattern.FindStringSubmatch(text)
	if match == nil {
		return 0, ErrNoNumber
	}
	wholeDigits, fractionDigits, unit := match[1], match[2], match[3]
	scale, known := multiplier(unit)
	if !known {
		if unit == "" {
			return 0, ErrNoUnit
		}
		return 0, fmt.Errorf("%w %q", ErrUnknownUnit, unit)
	}
	numerator := digitsToInt(wholeDigits + fractionDigits)
	numerator.Mul(numerator, big.NewInt(scale))
	denominator := new(big.Int).Exp(big.NewInt(decimalBase), big.NewInt(int64(len(fractionDigits))), nil)
	quotient, remainder := new(big.Int).QuoRem(numerator, denominator, new(big.Int))
	if remainder.Sign() != 0 {
		return 0, ErrNotWhole
	}
	if !quotient.IsInt64() || quotient.Int64() == math.MaxInt64 {
		return 0, ErrTooLarge
	}
	return quotient.Int64(), nil
}

// digitsToInt converts a string of decimal digits to a big integer.
func digitsToInt(digits string) *big.Int {
	number := new(big.Int)
	base := big.NewInt(decimalBase)
	for _, digit := range digits {
		number.Mul(number, base)
		number.Add(number, big.NewInt(int64(digit-'0')))
	}
	return number
}

// Size is a number of bytes. In YAML it is written as a whole number of bytes,
// as a number followed by KiB, MiB, GiB or TiB, or as the word "unlimited".
type Size int64

// UnlimitedSize is the Size that means no limit.
const UnlimitedSize Size = math.MaxInt64

const sizeAccepted = `use a whole number of bytes, a number followed by KiB, MiB, GiB or TiB, or "unlimited"`

// sizeUnit is a binary size suffix and the number of bytes it stands for.
type sizeUnit struct {
	suffix string
	bytes  Size
}

// Byte counts of the binary size units. They are untyped so they are not members of
// the Size enumeration that String switches over.
const (
	bytesPerKiB = 1 << 10
	bytesPerMiB = 1 << 20
	bytesPerGiB = 1 << 30
	bytesPerTiB = 1 << 40
)

// sizeUnits lists the accepted suffixes, largest first. It is a function so that no
// package-level variable can be changed by a caller.
func sizeUnits() [4]sizeUnit {
	return [...]sizeUnit{
		{"TiB", bytesPerTiB}, {"GiB", bytesPerGiB}, {"MiB", bytesPerMiB}, {"KiB", bytesPerKiB},
	}
}

// sizeMultiplier returns the number of bytes in a size unit. The empty unit
// means bytes.
func sizeMultiplier(unit string) (int64, bool) {
	if unit == "" {
		return 1, true
	}
	for _, candidate := range sizeUnits() {
		if candidate.suffix == unit {
			return int64(candidate.bytes), true
		}
	}
	return 0, false
}

// Unlimited reports whether s is UnlimitedSize.
func (s Size) Unlimited() bool { return s == UnlimitedSize }

// UnmarshalText parses a size such as "5MiB", "1.5GiB", "4096" or "unlimited".
func (s *Size) UnmarshalText(text []byte) error {
	value := string(text)
	if value == unlimitedWord {
		*s = UnlimitedSize
		return nil
	}
	bytes, err := parseScaled(value, sizeMultiplier)
	if err != nil {
		return fmt.Errorf("invalid size %q: %w (%s)", value, err, sizeAccepted)
	}
	*s = Size(bytes)
	return nil
}

// MarshalText renders the size as String does.
func (s Size) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

// String renders the size in the largest binary unit that represents it
// exactly, for example "5MiB" or "1536KiB". A size that is not a multiple of
// 1 KiB is rendered as a plain number of bytes.
func (s Size) String() string {
	switch s {
	case UnlimitedSize:
		return unlimitedWord
	case 0:
		return "0"
	}
	for _, unit := range sizeUnits() {
		if s%unit.bytes == 0 {
			return strconv.FormatInt(int64(s/unit.bytes), decimalBase) + unit.suffix
		}
	}
	return strconv.FormatInt(int64(s), decimalBase)
}

// Duration is a length of time. In YAML it is written as a number followed by
// s, m, h or d (a day of 24 hours), as 0, or as the word "unlimited".
// Compound values such as "1h30m" are not accepted.
type Duration time.Duration

// UnlimitedDuration is the Duration that means no limit.
const UnlimitedDuration Duration = math.MaxInt64

const durationAccepted = `use a number followed by s, m, h or d, or 0, or "unlimited"`

// durationUnit is a duration suffix and the length of time it stands for.
type durationUnit struct {
	suffix string
	length time.Duration
}

// hoursPerDay is the length of the "d" suffix. A day is always 24 hours here, with
// no regard for time zones.
const hoursPerDay = 24

// durationUnits lists the accepted suffixes, largest first. It is a function so that
// no package-level variable can be changed by a caller.
func durationUnits() [4]durationUnit {
	return [...]durationUnit{
		{"d", hoursPerDay * time.Hour}, {"h", time.Hour}, {"m", time.Minute}, {"s", time.Second},
	}
}

// durationMultiplier returns the number of nanoseconds in a duration unit.
func durationMultiplier(unit string) (int64, bool) {
	for _, candidate := range durationUnits() {
		if candidate.suffix == unit {
			return int64(candidate.length), true
		}
	}
	return 0, false
}

// Unlimited reports whether d is UnlimitedDuration.
func (d Duration) Unlimited() bool { return d == UnlimitedDuration }

// Std returns d as a time.Duration.
func (d Duration) Std() time.Duration { return time.Duration(d) }

// UnmarshalText parses a duration such as "30m", "1.5h", "7d", "0" or
// "unlimited".
func (d *Duration) UnmarshalText(text []byte) error {
	value := string(text)
	switch value {
	case unlimitedWord:
		*d = UnlimitedDuration
		return nil
	case "0":
		*d = 0
		return nil
	}
	nanoseconds, err := parseScaled(value, durationMultiplier)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w (%s)", value, err, durationAccepted)
	}
	*d = Duration(nanoseconds)
	return nil
}

// MarshalText renders the duration as String does.
func (d Duration) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

// String renders the duration in the largest unit of d, h, m or s that
// represents it exactly, for example "7d" or "90m". A duration that is not a
// whole number of seconds is rendered as decimal seconds, for example "1.5s".
// A negative duration, which cannot be written in a file, is rendered by
// time.Duration.String.
func (d Duration) String() string {
	switch {
	case d == UnlimitedDuration:
		return unlimitedWord
	case d == 0:
		return "0"
	case d < 0:
		return time.Duration(d).String()
	}
	for _, unit := range durationUnits() {
		if time.Duration(d)%unit.length == 0 {
			return strconv.FormatInt(int64(time.Duration(d)/unit.length), decimalBase) + unit.suffix
		}
	}
	whole, frac := time.Duration(d)/time.Second, time.Duration(d)%time.Second
	digits := strings.TrimRight(fmt.Sprintf("%09d", int64(frac)), "0")
	return fmt.Sprintf("%d.%ss", int64(whole), digits)
}
