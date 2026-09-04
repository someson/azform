// Package shell implements a quote-aware shell tokenizer and az-command buffer
// parser per azform spec §6.8.
package shell

import "strings"

// TokenKind classifies one lexical unit in a shell command line.
type TokenKind int

const (
	TokWord     TokenKind = iota // unquoted or quoted word
	TokOp                        // shell operator: && || ; | bare-newline
	TokCmdSubst                  // $(...) or `...`; Inner holds the raw content between delimiters
)

// Token is one lexical unit produced by Tokenize.
type Token struct {
	Kind     TokenKind
	Raw      string // exact bytes from source including delimiters/quotes
	Value    string // unescaped content; for TokCmdSubst equals Raw
	Inner    string // for TokCmdSubst: text between $( and ) or between backticks
	Start    int    // byte offset in source
	End      int    // exclusive byte offset
	Unclosed bool   // true when a quote or $(/`  delimiter was not closed before EOL
}

// Tokenize splits line into shell tokens, respecting single/double quoting,
// backslash escaping, line continuation (\<newline>), command substitution,
// and shell operators.
//
// Whitespace between tokens is consumed silently. Redirections (> < >>) are
// emitted as TokOp tokens so they terminate words, but their targets are not
// parsed as special (they become plain TokWord tokens).
func Tokenize(line string) []Token {
	var tokens []Token
	i := 0
	for i < len(line) {
		// horizontal whitespace
		if line[i] == ' ' || line[i] == '\t' {
			i++
			continue
		}
		// line continuation: \<newline> → skip both, continue word/whitespace scan
		if line[i] == '\\' && i+1 < len(line) && line[i+1] == '\n' {
			i += 2
			continue
		}
		// bare newline → operator (terminates a command)
		if line[i] == '\n' {
			tokens = append(tokens, Token{Kind: TokOp, Raw: "\n", Value: "\n", Start: i, End: i + 1})
			i++
			continue
		}
		// $( command substitution — may appear after bare chars (e.g. RG=$(…))
		if line[i] == '$' && i+1 < len(line) && line[i+1] == '(' {
			tok, n := scanCmdSubst(line, i)
			tokens = append(tokens, tok)
			i += n
			continue
		}
		// backtick command substitution
		if line[i] == '`' {
			tok, n := scanBacktick(line, i)
			tokens = append(tokens, tok)
			i += n
			continue
		}
		// two-char operators
		if i+1 < len(line) {
			switch line[i : i+2] {
			case "&&", "||", ">>":
				s := line[i : i+2]
				tokens = append(tokens, Token{Kind: TokOp, Raw: s, Value: s, Start: i, End: i + 2})
				i += 2
				continue
			}
		}
		// single-char operators
		switch line[i] {
		case ';', '|', '&', '>', '<':
			s := string(line[i])
			tokens = append(tokens, Token{Kind: TokOp, Raw: s, Value: s, Start: i, End: i + 1})
			i++
			continue
		}
		// word (bare, quoted, or mix)
		tok, n := scanWord(line, i)
		if n == 0 {
			i++ // safety
			continue
		}
		tokens = append(tokens, tok)
		i += n
	}
	return tokens
}

// scanWord reads one shell word starting at start. A word ends at unquoted
// whitespace, a bare operator, or a $( / ` that starts a command substitution
// at the word boundary.
func scanWord(line string, start int) (Token, int) {
	var raw strings.Builder
	var val strings.Builder
	unclosed := false
	i := start

	for i < len(line) {
		switch line[i] {
		case ' ', '\t', '\n':
			goto done
		case '\\':
			if i+1 < len(line) && line[i+1] == '\n' {
				goto done // line continuation ends the word
			}
			if i+1 < len(line) {
				raw.WriteByte('\\')
				raw.WriteByte(line[i+1])
				val.WriteByte(line[i+1])
				i += 2
			} else {
				raw.WriteByte('\\')
				val.WriteByte('\\')
				i++
			}
		case '\'':
			raw.WriteByte('\'')
			i++
			for i < len(line) && line[i] != '\'' {
				raw.WriteByte(line[i])
				val.WriteByte(line[i])
				i++
			}
			if i < len(line) {
				raw.WriteByte('\'')
				i++
			} else {
				unclosed = true
			}
		case '"':
			raw.WriteByte('"')
			i++
			for i < len(line) && line[i] != '"' {
				if line[i] == '\\' && i+1 < len(line) {
					next := line[i+1]
					switch next {
					case '"', '\\', '$', '`', '\n':
						raw.WriteByte('\\')
						raw.WriteByte(next)
						if next != '\n' {
							val.WriteByte(next)
						}
						i += 2
					default:
						raw.WriteByte(line[i])
						raw.WriteByte(next)
						val.WriteByte(line[i])
						val.WriteByte(next)
						i += 2
					}
				} else if line[i] == '$' && i+1 < len(line) && line[i+1] == '(' {
					// cmdsubst inside double-quotes: consume balanced parens verbatim
					j := i + 2
					depth := 0
					for j < len(line) {
						if line[j] == '(' {
							depth++
						} else if line[j] == ')' {
							if depth == 0 {
								j++
								break
							}
							depth--
						}
						j++
					}
					frag := line[i:j]
					raw.WriteString(frag)
					val.WriteString(frag)
					i = j
				} else {
					raw.WriteByte(line[i])
					val.WriteByte(line[i])
					i++
				}
			}
			if i < len(line) && line[i] == '"' {
				raw.WriteByte('"')
				i++
			} else {
				unclosed = true
			}
		case '$':
			if i+1 < len(line) && line[i+1] == '(' {
				goto done // CmdSubst starts at word boundary; handled at top level
			}
			raw.WriteByte('$')
			val.WriteByte('$')
			i++
		case '`':
			goto done // backtick CmdSubst handled at top level
		case ';', '|', '&', '>', '<':
			goto done
		default:
			if i+1 < len(line) {
				switch line[i : i+2] {
				case "&&", "||", ">>":
					goto done
				}
			}
			raw.WriteByte(line[i])
			val.WriteByte(line[i])
			i++
		}
	}
done:
	if raw.Len() == 0 {
		return Token{}, 0
	}
	return Token{
		Kind:     TokWord,
		Raw:      raw.String(),
		Value:    val.String(),
		Start:    start,
		End:      i,
		Unclosed: unclosed,
	}, i - start
}

// scanCmdSubst reads a $(...) token starting at start (which is '$').
// It tracks parenthesis depth, respecting single/double quotes inside.
func scanCmdSubst(line string, start int) (Token, int) {
	depth := 0
	i := start + 2 // skip "$("
	for i < len(line) {
		switch line[i] {
		case '(':
			depth++
		case ')':
			if depth == 0 {
				end := i + 1
				raw := line[start:end]
				inner := line[start+2 : i]
				return Token{Kind: TokCmdSubst, Raw: raw, Value: raw, Inner: inner, Start: start, End: end}, end - start
			}
			depth--
		case '\'':
			i++
			for i < len(line) && line[i] != '\'' {
				i++
			}
		case '"':
			i++
			for i < len(line) {
				if line[i] == '\\' && i+1 < len(line) {
					i += 2
				} else if line[i] == '"' {
					break
				} else {
					i++
				}
			}
		}
		i++
	}
	// unclosed — return everything
	raw := line[start:]
	inner := line[start+2:]
	return Token{Kind: TokCmdSubst, Raw: raw, Value: raw, Inner: inner, Start: start, End: len(line), Unclosed: true}, len(line) - start
}

// scanBacktick reads a `...` token starting at start.
func scanBacktick(line string, start int) (Token, int) {
	i := start + 1
	for i < len(line) && line[i] != '`' {
		if line[i] == '\\' && i+1 < len(line) {
			i += 2
		} else {
			i++
		}
	}
	end := i
	closed := i < len(line)
	if closed {
		end = i + 1
	}
	raw := line[start:end]
	inner := line[start+1 : i]
	return Token{Kind: TokCmdSubst, Raw: raw, Value: raw, Inner: inner, Start: start, End: end, Unclosed: !closed}, end - start
}
