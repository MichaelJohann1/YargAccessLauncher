package chartfile

import (
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The helpers below reproduce the Python text and number handling the readers were written against,
// so a chart reads the same in both launchers.

// pySpace is Python's str.isspace for one character.
func pySpace(r rune) bool {
	return unicode.IsSpace(r) || r >= 0x1C && r <= 0x1F
}

// pyStrip is Python's str.strip() with no argument.
func pyStrip(s string) string { return strings.TrimFunc(s, pySpace) }

// pySplitLines is Python's str.splitlines().
func pySplitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		switch r {
		case '\n', '\r', '\v', '\f', 0x1C, 0x1D, 0x1E, 0x85, 0x2028, 0x2029:
			out = append(out, s[start:i])
			i += n
			if r == '\r' && i < len(s) && s[i] == '\n' {
				i++
			}
			start = i
		default:
			i += n
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

// decodeUTF8Sig is Python's bytes.decode("utf-8-sig", "replace").
func decodeUTF8Sig(b []byte) string {
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		b = b[3:]
	}
	if utf8.Valid(b) {
		return string(b)
	}
	var sb strings.Builder
	for i := 0; i < len(b); {
		r, n := utf8.DecodeRune(b[i:])
		if r != utf8.RuneError || n > 1 {
			sb.WriteRune(r)
			i += n
			continue
		}
		sb.WriteRune(utf8.RuneError)
		i += invalidLen(b[i:])
	}
	return sb.String()
}

// invalidLen is the length of the maximal subpart of an ill-formed UTF-8 sequence at b[0], the unit
// Python replaces with one U+FFFD.
func invalidLen(b []byte) int {
	c := b[0]
	need, lo, hi := 0, byte(0x80), byte(0xBF)
	switch {
	case c >= 0xC2 && c <= 0xDF:
		need = 1
	case c == 0xE0:
		need, lo = 2, 0xA0
	case c == 0xED:
		need, hi = 2, 0x9F
	case c >= 0xE1 && c <= 0xEF:
		need = 2
	case c == 0xF0:
		need, lo = 3, 0x90
	case c >= 0xF1 && c <= 0xF3:
		need = 3
	case c == 0xF4:
		need, hi = 3, 0x8F
	default:
		return 1
	}
	k := 1
	for k <= need && k < len(b) {
		x := b[k]
		if k == 1 && (x < lo || x > hi) || k > 1 && (x < 0x80 || x > 0xBF) {
			break
		}
		k++
	}
	return k
}

// latin1 is Python's bytes.decode("latin-1"): each byte is the code point of the same value.
func latin1(b []byte) string {
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
	}
	return string(r)
}

// pyUpper is Python's str.upper() for latin-1 text (ß becomes SS; the other letters map one to one).
func pyUpper(s string) string {
	return strings.ToUpper(strings.ReplaceAll(s, "ß", "SS"))
}

// pyFloat is Python's float(str): surrounding whitespace allowed, underscores only between digits,
// inf / infinity / nan in any case, no hex. Out of range values become ±inf as in Python.
func pyFloat(s string) (float64, bool) {
	s = pyStrip(s)
	body := strings.TrimLeft(s, "+-")
	if len(s)-len(body) > 1 || body == "" {
		return 0, false
	}
	if len(body) > 1 && body[0] == '0' && (body[1] == 'x' || body[1] == 'X') {
		return 0, false
	}
	if strings.Contains(s, "_") {
		for i := 0; i < len(s); i++ {
			if s[i] != '_' {
				continue
			}
			if i == 0 || i == len(s)-1 || !isDigit(s[i-1]) || !isDigit(s[i+1]) {
				return 0, false
			}
		}
		s = strings.ReplaceAll(s, "_", "")
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(isDigit(c) || c == '.' || c == 'e' || c == 'E' || c == '+' || c == '-' ||
			c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
			return 0, false
		}
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		if ne, ok := err.(*strconv.NumError); ok && ne.Err == strconv.ErrRange {
			return v, true
		}
		return 0, false
	}
	return v, true
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// pyInt is the readers' _int: int(float(str(text).strip())), truncating toward zero; ok false where
// Python returns None. Python raises OverflowError for an infinite value; here that is None too.
// Values past ±2^62 are clamped (no chart comes near).
func pyInt(text string, present bool) (int, bool) {
	if !present {
		return 0, false
	}
	f, ok := pyFloat(text)
	if !ok || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	const lim = 1 << 62
	if f >= lim {
		return lim, true
	}
	if f <= -lim {
		return -lim, true
	}
	return int(math.Trunc(f)), true
}

// floorDiv is Python's // on ints.
func floorDiv(a, b int) int {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

// pyMod is Python's % on ints (the result has the divisor's sign).
func pyMod(a, b int) int {
	m := a % b
	if m != 0 && (m < 0) != (b < 0) {
		m += b
	}
	return m
}

// fsum is Python 3.12's sum() of floats (Neumaier compensated summation, starting at 0).
func fsum(xs []float64) float64 {
	s, c := 0.0, 0.0
	for _, x := range xs {
		t := s + x
		if math.Abs(s) >= math.Abs(x) {
			c += (s - t) + x
		} else {
			c += (x - t) + s
		}
		s = t
	}
	if c != 0 && !math.IsInf(c, 0) && !math.IsNaN(c) {
		s += c
	}
	return s
}

// pmax and pmin are Python's max(a, b) / min(a, b): the first argument wins unless the second
// compares greater (smaller), so NaN behaves as in Python.
func pmax(a, b float64) float64 {
	if b > a {
		return b
	}
	return a
}

func pmin(a, b float64) float64 {
	if b < a {
		return b
	}
	return a
}
