package tools

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"

	"github.com/mockagents/mockagents/demo/agent-playground/internal/llm"
)

// calculator evaluates arithmetic with a small recursive-descent parser:
// + - * / ^, unary minus and parentheses. It never calls eval, which matters
// because tool arguments are untrusted model output.
type calculator struct{}

func (calculator) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "calculator",
		Description: "Evaluate an arithmetic expression (+ - * / ^ and parentheses).",
		Parameters: schema([]string{"expression"}, map[string]any{
			"expression": map[string]any{"type": "string", "description": "For example 200 * 2^4."},
		}),
	}
}

func (calculator) RequiresApproval() bool { return false }

func (calculator) Execute(_ context.Context, args map[string]any) (any, error) {
	expr := str(args, "expression")
	if len(expr) > 200 {
		return nil, fmt.Errorf("expression too long")
	}
	v, err := Evaluate(expr)
	if err != nil {
		return nil, err
	}
	return map[string]any{"expression": expr, "result": FormatNumber(v)}, nil
}

// FormatNumber renders a float compactly (3200, 0.06).
func FormatNumber(v float64) string {
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// Evaluate parses and evaluates an arithmetic expression.
func Evaluate(expr string) (float64, error) {
	p := &parser{src: expr}
	v, err := p.expr()
	if err != nil {
		return 0, err
	}
	p.skip()
	if p.pos != len(p.src) {
		return 0, fmt.Errorf("unexpected %q at position %d", p.src[p.pos:], p.pos)
	}
	if math.IsInf(v, 0) || math.IsNaN(v) {
		return 0, fmt.Errorf("result is not a finite number")
	}
	return v, nil
}

type parser struct {
	src   string
	pos   int
	depth int
}

func (p *parser) skip() {
	for p.pos < len(p.src) && unicode.IsSpace(rune(p.src[p.pos])) {
		p.pos++
	}
}

func (p *parser) peek() byte {
	p.skip()
	if p.pos < len(p.src) {
		return p.src[p.pos]
	}
	return 0
}

// expr := term (('+'|'-') term)*
func (p *parser) expr() (float64, error) {
	v, err := p.term()
	if err != nil {
		return 0, err
	}
	for {
		switch p.peek() {
		case '+':
			p.pos++
			r, err := p.term()
			if err != nil {
				return 0, err
			}
			v += r
		case '-':
			p.pos++
			r, err := p.term()
			if err != nil {
				return 0, err
			}
			v -= r
		default:
			return v, nil
		}
	}
}

// term := power (('*'|'/') power)*
func (p *parser) term() (float64, error) {
	v, err := p.power()
	if err != nil {
		return 0, err
	}
	for {
		switch p.peek() {
		case '*':
			p.pos++
			r, err := p.power()
			if err != nil {
				return 0, err
			}
			v *= r
		case '/':
			p.pos++
			r, err := p.power()
			if err != nil {
				return 0, err
			}
			if r == 0 {
				return 0, fmt.Errorf("division by zero")
			}
			v /= r
		default:
			return v, nil
		}
	}
}

// power := unary ('^' power)?   (right-associative)
func (p *parser) power() (float64, error) {
	base, err := p.unary()
	if err != nil {
		return 0, err
	}
	if p.peek() == '^' {
		p.pos++
		exp, err := p.power()
		if err != nil {
			return 0, err
		}
		return math.Pow(base, exp), nil
	}
	return base, nil
}

// unary := '-' unary | primary
func (p *parser) unary() (float64, error) {
	if p.peek() == '-' {
		p.pos++
		v, err := p.unary()
		return -v, err
	}
	return p.primary()
}

// primary := number | '(' expr ')'
func (p *parser) primary() (float64, error) {
	c := p.peek()
	if c == '(' {
		p.depth++
		if p.depth > 32 {
			return 0, fmt.Errorf("expression nested too deeply")
		}
		p.pos++
		v, err := p.expr()
		if err != nil {
			return 0, err
		}
		if p.peek() != ')' {
			return 0, fmt.Errorf("missing closing parenthesis")
		}
		p.pos++
		p.depth--
		return v, nil
	}
	start := p.pos
	for p.pos < len(p.src) && (p.src[p.pos] >= '0' && p.src[p.pos] <= '9' || p.src[p.pos] == '.') {
		p.pos++
	}
	if start == p.pos {
		if p.pos >= len(p.src) {
			return 0, fmt.Errorf("unexpected end of expression")
		}
		return 0, fmt.Errorf("unexpected %q at position %d", string(p.src[p.pos]), p.pos)
	}
	return strconv.ParseFloat(strings.TrimSpace(p.src[start:p.pos]), 64)
}
