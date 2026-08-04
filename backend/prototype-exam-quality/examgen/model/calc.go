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
func choiceMentionsNumber(choice string, v float64) bool {
	candidates := []string{
		trimZeros(fmt.Sprintf("%.6f", v)), trimZeros(fmt.Sprintf("%.4f", v)),
		trimZeros(fmt.Sprintf("%.3f", v)), trimZeros(fmt.Sprintf("%.2f", v)),
		trimZeros(fmt.Sprintf("%.1f", v)), fmt.Sprintf("%.0f", v), fmt.Sprintf("%g", v),
	}
	normalized := normalizeScientificNotation(choice)
	for _, format := range []string{"%.0e", "%.1e", "%.2e", "%.3e", "%.4e"} {
		candidates = append(candidates, normalizeScientificLiteral(fmt.Sprintf(format, v)))
	}
	haystack := strings.ReplaceAll(choice, ",", "")
	for _, candidate := range candidates {
		if candidate != "" && strings.Contains(haystack, candidate) {
			return true
		}
	}
	if v >= 0 && v <= 20 && math.Abs(v-math.Round(v)) < 1e-9 {
		words := []string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten", "eleven", "twelve", "thirteen", "fourteen", "fifteen", "sixteen", "seventeen", "eighteen", "nineteen", "twenty"}
		if strings.Contains(strings.ToLower(choice), words[int(math.Round(v))]) {
			return true
		}
	}
	for _, format := range []string{"%.0e", "%.1e", "%.2e", "%.3e", "%.4e"} {
		candidate := normalizeScientificLiteral(fmt.Sprintf(format, v))
		if candidate != "" && strings.Contains(normalized, candidate) {
			return true
		}
	}
	return false
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
		case '⁰', '¹', '²', '³', '⁴', '⁵', '⁶', '⁷', '⁸', '⁹':
			b.WriteString(strconv.Itoa(int(r - '⁰')))
		default:
			return 0, false
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
