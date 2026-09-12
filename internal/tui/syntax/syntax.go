// Package syntax returns Chroma tokens rather than rendered text, so the caller owns the row.
package syntax

import (
	"hash/fnv"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

// Token is one run of code in one color. Color is nil where the style says nothing.
type Token struct {
	Text  string
	Color color.Color
}

// Syntax tokenises source and caches the result. Not safe for concurrent use.
type Syntax struct {
	style *chroma.Style
	cache map[uint64][][]Token
}

// New builds a Syntax over a Chroma style, reporting whether the name was known.
// An unknown name still colors.
func New(name string) (Syntax, bool) {
	_, ok := styles.Registry[name]
	return Syntax{style: styles.Get(name), cache: make(map[uint64][][]Token)}, ok || name == ""
}

// Lines tokenises code whole with a lexer chosen from path, split into at least one line.
func (s *Syntax) Lines(path, code string) [][]Token {
	h := fnv.New64a()
	_, _ = h.Write([]byte(path))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(code))
	key := h.Sum64()

	if out, ok := s.cache[key]; ok {
		return out
	}

	out := s.tokenise(path, code)
	s.cache[key] = out
	return out
}

func (s *Syntax) tokenise(path, code string) [][]Token {
	lexer := lexers.Match(path)
	if lexer == nil {
		lexer = lexers.Fallback
	}

	iter, err := chroma.Coalesce(lexer).Tokenise(nil, code)
	if err != nil {
		return plain(code)
	}

	var out [][]Token
	for _, line := range chroma.SplitTokensIntoLines(iter.Tokens()) {
		row := make([]Token, 0, len(line))
		for _, t := range line {
			text := strings.TrimSuffix(t.Value, "\n")
			if text == "" {
				continue
			}
			row = append(row, Token{Text: text, Color: colorOf(s.style.Get(t.Type).Colour)})
		}
		out = append(out, row)
	}

	if out == nil {
		out = [][]Token{{}}
	}
	return out
}

func plain(code string) [][]Token {
	lines := strings.Split(code, "\n")
	out := make([][]Token, len(lines))
	for i, line := range lines {
		out[i] = []Token{{Text: line}}
	}
	return out
}

func colorOf(c chroma.Colour) color.Color {
	if !c.IsSet() {
		return nil
	}
	return lipgloss.Color(c.String())
}
