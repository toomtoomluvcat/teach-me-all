package examgen

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Arith is a deliberately tiny arithmetic evaluator: numbers, + - * / ^, unary
// sign, and parentheses. Nothing else parses.
//
// This is hand-written rather than pulled from an expression library on
// purpose. The expressions it evaluates are written by a language model and
// will be stored in the database and re-evaluated for years. A grammar that
// cannot express a function call, a variable, or a property access cannot be
// talked into doing anything except arithmetic, no matter what the model emits.
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

// primary := number | '(' expr ')'
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
	return p.number()
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
