package adminquery

import (
	"fmt"
	"strings"
	"unicode"
)

type Op string

const (
	OpEq  Op = "="
	OpNe  Op = "!="
	OpLt  Op = "<"
	OpLe  Op = "<="
	OpGt  Op = ">"
	OpGe  Op = ">="
	OpHas Op = ":"
)

type node interface{ isNode() }

type comparison struct {
	field string
	op    Op
	value string
}

type and struct{ terms []node }

type or struct{ terms []node }

type not struct{ term node }

func (comparison) isNode() {}
func (and) isNode()        {}
func (or) isNode()         {}
func (not) isNode()        {}

const (
	maxFilterLength = 1000
	maxComparisons  = 24
	maxDepth        = 4
)

type tokenKind int

const (
	tokenWord tokenKind = iota
	tokenString
	tokenOp
	tokenOpen
	tokenClose
	tokenEnd
)

type token struct {
	kind tokenKind
	text string
	pos  int
}

func lex(input string) ([]token, error) {
	var out []token
	runes := []rune(input)
	for i := 0; i < len(runes); {
		r := runes[i]
		switch {
		case unicode.IsSpace(r):
			i++
		case r == '(':
			out = append(out, token{kind: tokenOpen, text: "(", pos: i})
			i++
		case r == ')':
			out = append(out, token{kind: tokenClose, text: ")", pos: i})
			i++
		case r == '"':
			var b strings.Builder
			start := i
			i++
			closed := false
			for i < len(runes) {
				c := runes[i]
				if c == '\\' && i+1 < len(runes) {
					b.WriteRune(runes[i+1])
					i += 2
					continue
				}
				if c == '"' {
					closed = true
					i++
					break
				}
				b.WriteRune(c)
				i++
			}
			if !closed {
				return nil, fmt.Errorf("unterminated string at %d", start)
			}
			out = append(out, token{kind: tokenString, text: b.String(), pos: start})
		case r == '=' || r == ':':
			out = append(out, token{kind: tokenOp, text: string(r), pos: i})
			i++
		case r == '!' || r == '<' || r == '>':
			if i+1 < len(runes) && runes[i+1] == '=' {
				out = append(out, token{kind: tokenOp, text: string(r) + "=", pos: i})
				i += 2
				continue
			}
			if r == '!' {
				return nil, fmt.Errorf("unexpected '!' at %d", i)
			}
			out = append(out, token{kind: tokenOp, text: string(r), pos: i})
			i++
		default:
			start := i
			for i < len(runes) && !unicode.IsSpace(runes[i]) && !strings.ContainsRune(`()"=:!<>`, runes[i]) {
				i++
			}
			out = append(out, token{kind: tokenWord, text: string(runes[start:i]), pos: start})
		}
	}
	return append(out, token{kind: tokenEnd, pos: len(runes)}), nil
}

type parser struct {
	tokens      []token
	at          int
	comparisons int
}

func parse(input string) (node, error) {
	if strings.TrimSpace(input) == "" {
		return nil, nil
	}
	if len(input) > maxFilterLength {
		return nil, fmt.Errorf("is longer than %d characters", maxFilterLength)
	}
	tokens, err := lex(input)
	if err != nil {
		return nil, err
	}
	p := &parser{tokens: tokens}
	n, err := p.expression(0)
	if err != nil {
		return nil, err
	}
	if t := p.peek(); t.kind != tokenEnd {
		return nil, fmt.Errorf("unexpected %q at %d", t.text, t.pos)
	}
	return n, nil
}

func (p *parser) peek() token { return p.tokens[p.at] }

func (p *parser) next() token {
	t := p.tokens[p.at]
	if t.kind != tokenEnd {
		p.at++
	}
	return t
}

func keyword(t token, word string) bool {
	return t.kind == tokenWord && t.text == word
}

func (p *parser) expression(depth int) (node, error) {
	if depth > maxDepth {
		return nil, fmt.Errorf("is nested deeper than %d levels", maxDepth)
	}
	var terms []node
	for {
		term, err := p.disjunction(depth)
		if err != nil {
			return nil, err
		}
		terms = append(terms, term)
		t := p.peek()
		if keyword(t, "AND") {
			p.next()
			continue
		}
		if t.kind == tokenEnd || t.kind == tokenClose {
			break
		}
	}
	if len(terms) == 1 {
		return terms[0], nil
	}
	return and{terms: terms}, nil
}

func (p *parser) disjunction(depth int) (node, error) {
	var terms []node
	for {
		term, err := p.term(depth)
		if err != nil {
			return nil, err
		}
		terms = append(terms, term)
		if !keyword(p.peek(), "OR") {
			break
		}
		p.next()
	}
	if len(terms) == 1 {
		return terms[0], nil
	}
	return or{terms: terms}, nil
}

func (p *parser) term(depth int) (node, error) {
	t := p.peek()
	if keyword(t, "NOT") {
		p.next()
		inner, err := p.term(depth)
		if err != nil {
			return nil, err
		}
		return not{term: inner}, nil
	}
	if t.kind == tokenOpen {
		p.next()
		inner, err := p.expression(depth + 1)
		if err != nil {
			return nil, err
		}
		if c := p.next(); c.kind != tokenClose {
			return nil, fmt.Errorf("expected ')' at %d", c.pos)
		}
		return inner, nil
	}
	return p.comparison()
}

func (p *parser) comparison() (node, error) {
	field := p.next()
	if field.kind != tokenWord || keyword(field, "AND") || keyword(field, "OR") {
		return nil, fmt.Errorf("expected a field name at %d", field.pos)
	}
	op := p.next()
	if op.kind != tokenOp {
		return nil, fmt.Errorf("expected an operator after %q", field.text)
	}
	value := p.next()
	if value.kind != tokenWord && value.kind != tokenString {
		return nil, fmt.Errorf("expected a value after %q %s", field.text, op.text)
	}
	p.comparisons++
	if p.comparisons > maxComparisons {
		return nil, fmt.Errorf("has more than %d conditions", maxComparisons)
	}
	return comparison{field: field.text, op: Op(op.text), value: value.text}, nil
}

type orderTerm struct {
	field string
	desc  bool
}

func parseOrder(input string) ([]orderTerm, error) {
	if strings.TrimSpace(input) == "" {
		return nil, nil
	}
	if len(input) > 200 {
		return nil, fmt.Errorf("is longer than 200 characters")
	}
	var out []orderTerm
	seen := map[string]bool{}
	for _, part := range strings.Split(input, ",") {
		words := strings.Fields(part)
		if len(words) == 0 || len(words) > 2 {
			return nil, fmt.Errorf("has an invalid term %q", strings.TrimSpace(part))
		}
		term := orderTerm{field: words[0]}
		if len(words) == 2 {
			switch strings.ToLower(words[1]) {
			case "desc":
				term.desc = true
			case "asc":
			default:
				return nil, fmt.Errorf("has an invalid direction %q", words[1])
			}
		}
		if seen[term.field] {
			return nil, fmt.Errorf("repeats %q", term.field)
		}
		seen[term.field] = true
		out = append(out, term)
	}
	return out, nil
}
