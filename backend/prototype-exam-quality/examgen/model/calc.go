package model

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// choiceMentionsNumber accepts the rounded and scientific-number forms that
// appear in natural-language answer choices.
//
// A choice that states the value losslessly — including ordinary rounding
// that does not meaningfully change it, like 35.348 for a computed
// 35.34795918... — always matches. A choice that only matches after throwing
// away real precision, like 1.7 for a computed 1.67, only matches when the
// choice itself says it is approximate ("≈", "about", "ประมาณ", ...).
// Otherwise a choice claiming an exact answer that is actually short a digit
// reads as correct when it silently isn't.
func choiceMentionsNumber(choice string, v float64) bool {
	haystack := strings.ReplaceAll(choice, ",", "")
	approx := hasApproxMarker(choice)

	decimalCandidates := []string{
		fmt.Sprintf("%.0f", v),
		trimZeros(fmt.Sprintf("%.1f", v)), fmt.Sprintf("%.1f", v),
		trimZeros(fmt.Sprintf("%.2f", v)), fmt.Sprintf("%.2f", v),
		trimZeros(fmt.Sprintf("%.3f", v)), fmt.Sprintf("%.3f", v),
		trimZeros(fmt.Sprintf("%.4f", v)), fmt.Sprintf("%.4f", v),
		trimZeros(fmt.Sprintf("%.6f", v)), fmt.Sprintf("%.6f", v),
		fmt.Sprintf("%g", v),
	}
	for _, text := range decimalCandidates {
		if text != "" && isLosslessRounding(v, text) && containsNumericToken(haystack, text) {
			return true
		}
	}
	if approx {
		for _, text := range decimalCandidates {
			if text != "" && containsNumericToken(haystack, text) {
				return true
			}
		}
	}

	if v >= 0 && v <= 20 && math.Abs(v-math.Round(v)) < 1e-9 {
		words := []string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten", "eleven", "twelve", "thirteen", "fourteen", "fifteen", "sixteen", "seventeen", "eighteen", "nineteen", "twenty"}
		if containsWordToken(strings.ToLower(choice), words[int(math.Round(v))]) {
			return true
		}
	}

	// Scientific notation is its own convention: a mantissa is expected to be
	// rounded to a handful of significant figures by definition, so it is not
	// held to the same "must say approximate" rule as plain decimals.
	normalized := normalizeScientificNotation(choice)
	for _, format := range []string{"%.0e", "%.1e", "%.2e", "%.3e", "%.4e"} {
		candidate := normalizeScientificLiteral(fmt.Sprintf(format, v))
		if candidate != "" && (containsNumericToken(haystack, candidate) || containsNumericToken(normalized, candidate)) {
			return true
		}
	}
	return false
}

// isLosslessRounding reports whether formatting v as text and parsing it back
// reproduces v closely enough that no meaningful precision was discarded.
// The tolerance is relative, not absolute, matching ordinary three-significant-
// figure rounding at any magnitude: "69.9" for a computed 69.914778 (0.02%
// off) is normal scientific convention, same as "35.348" for 35.34795918...
// (0.001% off). An absolute epsilon calibrated near magnitude ~1-10 (an
// earlier version of this check used 5e-4 absolute) silently stops working at
// different scales — it wrongly rejected legitimate percentage-scale rounding
// like "69.9%" while a jump like 1.67 -> "1.7" (1.8% relative, only 2 sig
// figs) is well outside 3-sig-fig tolerance at any scale and correctly still
// requires the choice to mark itself approximate.
func isLosslessRounding(v float64, text string) bool {
	parsed, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return false
	}
	if parsed == v {
		return true
	}
	if v == 0 {
		return math.Abs(parsed) < 1e-3
	}
	return math.Abs(parsed-v)/math.Abs(v) < 1e-3
}

func hasApproxMarker(choice string) bool {
	lower := strings.ToLower(choice)
	for _, marker := range []string{"≈", "~", "about ", "approx", "roughly", "around ", "ประมาณ", "โดยประมาณ"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func containsNumericToken(text, candidate string) bool {
	if candidate == "" {
		return false
	}
	start := 0
	for start < len(text) {
		rel := strings.Index(text[start:], candidate)
		if rel < 0 {
			return false
		}
		i := start + rel
		beforeOK := i == 0 || !isNumericAdjacent(text[i-1])
		end := i + len(candidate)
		afterOK := end >= len(text) || !isNumericAdjacent(text[end])
		if beforeOK && afterOK {
			return true
		}
		start = i + 1
	}
	return false
}

func isNumericAdjacent(b byte) bool {
	return (b >= '0' && b <= '9') || b == '.'
}

func containsWordToken(text, word string) bool {
	if word == "" {
		return false
	}
	start := 0
	for start < len(text) {
		rel := strings.Index(text[start:], word)
		if rel < 0 {
			return false
		}
		i := start + rel
		beforeOK := i == 0 || !isASCIIWord(text[i-1])
		end := i + len(word)
		afterOK := end >= len(text) || !isASCIIWord(text[end])
		if beforeOK && afterOK {
			return true
		}
		start = i + 1
	}
	return false
}

func isASCIIWord(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_'
}

// ChoiceMentionsNumber exposes the answer-choice number matcher to the gate
// package without moving numeric-formatting logic into orchestration code.
func ChoiceMentionsNumber(choice string, v float64) bool { return choiceMentionsNumber(choice, v) }

var scientificNotationPattern = regexp.MustCompile(`(?i)([-+]?(?:\d+(?:[.,]\d*)?|\.\d+))\s*(?:×|x|·|\*)\s*10\s*(?:\^\s*)?([+-]?\d+|[⁰¹²³⁴⁵⁶⁷⁸⁹⁺⁻]+)`)

func normalizeScientificNotation(s string) string {
	return scientificNotationPattern.ReplaceAllStringFunc(s, func(match string) string {
		parts := scientificNotationPattern.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		exponent, ok := parseExponent(parts[2])
		if !ok {
			return match
		}
		return strings.ReplaceAll(parts[1], ",", "") + "e" + strconv.Itoa(exponent)
	})
}

func normalizeScientificLiteral(s string) string {
	parts := strings.SplitN(strings.ToLower(s), "e", 2)
	if len(parts) != 2 {
		return s
	}
	exponent, err := strconv.Atoi(parts[1])
	if err != nil {
		return s
	}
	return parts[0] + "e" + strconv.Itoa(exponent)
}

// superscriptDigits maps each superscript digit to its ASCII value by direct
// lookup rather than arithmetic offset from '⁰'. Superscript 1/2/3 live in
// the Latin-1 Supplement block (U+00B9/B2/B3) while 0 and 4-9 live in the
// Superscripts and Subscripts block (U+2070, U+2074-2079) — a uniform '⁰'
// offset silently produces nonsense for 1/2/3 (e.g. '²' - '⁰' computes to
// -8126, not 2) instead of failing loudly, so a wrong exponent parsed clean
// as if it were valid.
var superscriptDigits = map[rune]byte{
	'⁰': '0', '¹': '1', '²': '2', '³': '3', '⁴': '4',
	'⁵': '5', '⁶': '6', '⁷': '7', '⁸': '8', '⁹': '9',
}

func parseExponent(s string) (int, bool) {
	if exponent, err := strconv.Atoi(s); err == nil {
		return exponent, true
	}
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '⁺':
			b.WriteRune('+')
		case '⁻':
			b.WriteRune('-')
		default:
			digit, ok := superscriptDigits[r]
			if !ok {
				return 0, false
			}
			b.WriteByte(digit)
		}
	}
	exponent, err := strconv.Atoi(b.String())
	return exponent, err == nil
}

func trimZeros(s string) string {
	if !strings.Contains(s, ".") {
		return s
	}
	return strings.TrimSuffix(strings.TrimRight(s, "0"), ".")
}

// Arith is a deliberately small, safe arithmetic evaluator. It accepts
// numbers, + - * / ^, unary sign, parentheses, pi, and a short whitelist of
// math functions commonly used in textbook calculations. Nothing else parses.
//
// This is hand-written rather than pulled from an expression library on
// purpose. The expressions it evaluates are written by a language model and
// will be stored in the database and re-evaluated for years. A grammar with
// no variables, property access, or arbitrary calls cannot be talked into
// doing anything except the explicitly supported arithmetic.
type Arith struct{}

// Eval implements Evaluator.
func (Arith) Eval(expr string) (float64, error) {
	p := &parser{src: []rune(stripThousands(expr))}
	p.skipSpace()
	if p.eof() {
		return 0, fmt.Errorf("empty expression")
	}
	v, err := p.expr()
	if err != nil {
		return 0, err
	}
	p.skipSpace()
	if !p.eof() {
		return 0, fmt.Errorf("unexpected %q at position %d", string(p.src[p.i]), p.i)
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, fmt.Errorf("expression produced %v", v)
	}
	return v, nil
}

// stripThousands removes a comma only where it is unambiguously a thousands
// separator: a digit, a comma, then exactly three digits not followed by
// another digit. "1,200" becomes "1200"; "1,2" is left alone so it fails to
// parse rather than silently becoming 12.
func stripThousands(s string) string {
	r := []rune(s)
	var b strings.Builder
	for i := 0; i < len(r); i++ {
		if r[i] == ',' && i > 0 && isDigit(r[i-1]) && i+3 < len(r)+1 {
			if i+3 < len(r)+1 && allDigits(r, i+1, i+4) && (i+4 >= len(r) || !isDigit(r[i+4])) {
				continue
			}
		}
		b.WriteRune(r[i])
	}
	return b.String()
}

func allDigits(r []rune, from, to int) bool {
	if to > len(r) {
		return false
	}
	for i := from; i < to; i++ {
		if !isDigit(r[i]) {
			return false
		}
	}
	return true
}

func isDigit(r rune) bool { return r >= '0' && r <= '9' }

type parser struct {
	src []rune
	i   int
}

func (p *parser) eof() bool { return p.i >= len(p.src) }

func (p *parser) skipSpace() {
	for !p.eof() && (p.src[p.i] == ' ' || p.src[p.i] == '\t' || p.src[p.i] == '\n' || p.src[p.i] == '\r') {
		p.i++
	}
}

func (p *parser) peek() rune {
	if p.eof() {
		return 0
	}
	return p.src[p.i]
}

// expr := term (('+' | '-') term)*
func (p *parser) expr() (float64, error) {
	v, err := p.term()
	if err != nil {
		return 0, err
	}
	for {
		p.skipSpace()
		op := p.peek()
		if op != '+' && op != '-' {
			return v, nil
		}
		p.i++
		rhs, err := p.term()
		if err != nil {
			return 0, err
		}
		if op == '+' {
			v += rhs
		} else {
			v -= rhs
		}
	}
}

// term := unary (('*' | '/') unary)*
func (p *parser) term() (float64, error) {
	v, err := p.unary()
	if err != nil {
		return 0, err
	}
	for {
		p.skipSpace()
		op := p.peek()
		// U+00D7 and U+00F7 show up in textbook-flavoured output often enough
		// to be worth accepting.
		if op != '*' && op != '/' && op != '×' && op != '÷' {
			return v, nil
		}
		p.i++
		rhs, err := p.unary()
		if err != nil {
			return 0, err
		}
		if op == '*' || op == '×' {
			v *= rhs
			continue
		}
		if rhs == 0 {
			return 0, fmt.Errorf("division by zero")
		}
		v /= rhs
	}
}

// unary := ('+' | '-')* power
func (p *parser) unary() (float64, error) {
	p.skipSpace()
	switch p.peek() {
	case '-':
		p.i++
		v, err := p.unary()
		return -v, err
	case '+':
		p.i++
		return p.unary()
	}
	return p.power()
}

// power := primary (('^' | '**') unary)?  — right associative
//
// '**' is accepted because models reach for Python syntax unprompted; a
// perfectly good compound-interest question was thrown away over it.
func (p *parser) power() (float64, error) {
	base, err := p.primary()
	if err != nil {
		return 0, err
	}
	p.skipSpace()

	switch {
	case p.peek() == '^':
		p.i++
	case p.peek() == '*' && p.i+1 < len(p.src) && p.src[p.i+1] == '*':
		p.i += 2
	default:
		return base, nil
	}

	exp, err := p.unary()
	if err != nil {
		return 0, err
	}
	return math.Pow(base, exp), nil
}

// primary := number | constant | function '(' expr ')' | '(' expr ')'
func (p *parser) primary() (float64, error) {
	p.skipSpace()
	if p.eof() {
		return 0, fmt.Errorf("expression ends where a number was expected")
	}
	if p.peek() == '(' {
		p.i++
		v, err := p.expr()
		if err != nil {
			return 0, err
		}
		p.skipSpace()
		if p.peek() != ')' {
			return 0, fmt.Errorf("missing closing parenthesis")
		}
		p.i++
		return v, nil
	}
	if isLetter(p.peek()) {
		name := p.identifier()
		if name == "pi" {
			return math.Pi, nil
		}
		if p.peek() != '(' {
			return 0, fmt.Errorf("unknown identifier %q", name)
		}
		p.i++
		arg, err := p.expr()
		if err != nil {
			return 0, err
		}
		p.skipSpace()
		if p.peek() != ')' {
			return 0, fmt.Errorf("missing closing parenthesis for %s", name)
		}
		p.i++
		return applyFunction(name, arg)
	}
	return p.number()
}

func (p *parser) identifier() string {
	start := p.i
	for !p.eof() && (isLetter(p.peek()) || isDigit(p.peek()) || p.peek() == '_') {
		p.i++
	}
	return strings.ToLower(string(p.src[start:p.i]))
}

func isLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func applyFunction(name string, arg float64) (float64, error) {
	switch name {
	case "sin":
		return math.Sin(arg), nil
	case "cos":
		return math.Cos(arg), nil
	case "tan":
		return math.Tan(arg), nil
	case "sqrt":
		if arg < 0 {
			return 0, fmt.Errorf("sqrt domain error")
		}
		return math.Sqrt(arg), nil
	case "abs":
		return math.Abs(arg), nil
	case "exp":
		return math.Exp(arg), nil
	case "ln":
		if arg <= 0 {
			return 0, fmt.Errorf("ln domain error")
		}
		return math.Log(arg), nil
	default:
		return 0, fmt.Errorf("unsupported function %q", name)
	}
}

func (p *parser) number() (float64, error) {
	start := p.i
	for !p.eof() {
		r := p.src[p.i]
		if isDigit(r) || r == '.' {
			p.i++
			continue
		}
		// Scientific notation, but only immediately after a digit.
		if (r == 'e' || r == 'E') && p.i > start {
			next := p.i + 1
			if next < len(p.src) && (p.src[next] == '+' || p.src[next] == '-') {
				next++
			}
			if next < len(p.src) && isDigit(p.src[next]) {
				p.i = next + 1
				continue
			}
		}
		break
	}
	if p.i == start {
		return 0, fmt.Errorf("expected a number at position %d, found %q", start, string(p.src[start]))
	}
	text := string(p.src[start:p.i])
	v, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not a number", text)
	}
	return v, nil
}
