// Package syntax colors source code. It hands back tokens rather than rendered
// text, because the caller owns the rest of the row: a diff paints a background
// per cell, and a token that rendered itself would end in a reset and tear a
// hole in it.
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

// Token is one run of code sharing a color. Color is nil where the style has
// nothing to say, which is most of the punctuation and whitespace in a file.
type Token struct {
	Text  string
	Color color.Color
}

// Syntax colors source code, returning tokens rather than rendered text: one
// that styled itself would reset the row's background. Lines mutates the cache.
type Syntax struct {
	style *chroma.Style
	cache map[uint64][][]Token
}

// New builds a colorizer over a Chroma style, reporting whether Chroma knew the
// name. An unknown one still colorizes, so a typo costs colors and not the diff.
func New(name string) (Syntax, bool) {
	_, ok := styles.Registry[name]
	return Syntax{style: styles.Get(name), cache: make(map[uint64][][]Token)}, ok || name == ""
}

// Lines splits code into lines of colored tokens, lexer chosen from the path and
// the body tokenised whole. Always at least one line, so an empty side indexes.
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
			// Foreground only: a Chroma style carries its own background, and
			// taking it would paint over the terminal's.
			text := strings.TrimSuffix(t.Value, "\n")
			if text == "" {
				continue
			}
			row = append(row, Token{Text: text, Color: colorOf(s.style.Get(t.Type).Colour)})
		}
		out = append(out, row)
	}

	// Chroma yields nothing for an empty body, but an empty file is one empty
	// line, which is what a caller walking a side against its rows counts on.
	if out == nil {
		out = [][]Token{{}}
	}
	return out
}

// plain is the fallback when a lexer fails outright: uncolored code beats no
// code.
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
