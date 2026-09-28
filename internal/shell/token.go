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
	// Subst is set on a fish word that contains an unquoted (…) command
	// substitution. Value then holds the substitution verbatim, so the word
	// must be re-emitted as typed (Raw), never quoted as a literal.
	Subst bool
}

// Syntax selects the quoting rules Tokenize applies. The two differ only
// inside single quotes: POSIX has no escapes there (a quote always closes
// it), while fish treats \\ and \' as escapes.
type Syntax int

const (
	POSIX Syntax = iota
	Fish
)

// Tokenize splits line into POSIX shell tokens; see TokenizeSyntax.
func Tokenize(line string) []Token { return TokenizeSyntax(line, POSIX) }

// TokenizeSyntax splits line into shell tokens, respecting single/double quoting,
// backslash escaping, line continuation (\<newline>), command substitution,
// and shell operators.
//
// Whitespace between tokens is consumed silently. Redirections (> < >>) are
// emitted as TokOp tokens so they terminate words, but their targets are not
// parsed as special (they become plain TokWord tokens).
func TokenizeSyntax(line string, syn Syntax) []Token {
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
			tok, n := scanCmdSubst(line, i, syn)
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
		tok, n := scanWord(line, i, syn)
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
func scanWord(line string, start int, syn Syntax) (Token, int) {
	var raw strings.Builder
	var val strings.Builder
	unclosed := false
	subst := false
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
				if syn == Fish && line[i] == '\\' && i+1 < len(line) &&
					(line[i+1] == '\'' || line[i+1] == '\\') {
					raw.WriteByte('\\')
					raw.WriteByte(line[i+1])
					val.WriteByte(line[i+1])
					i += 2
					continue
				}
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
					// fish does not escape the backtick in double quotes
					// (it has no backtick substitution), so \` stays two
					// characters there.
					switch {
					case next == '"' || next == '\\' || next == '$' || next == '\n',
						next == '`' && syn != Fish:
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
		case '(':
			if syn != Fish {
				raw.WriteByte('(')
				val.WriteByte('(')
				i++
				continue
			}
			// fish command substitution: (cmd) anywhere in a word, e.g.
			// (whoami)-rg. Copied verbatim, spaces and all, so the word
			// survives as one token and is re-emitted exactly as typed.
			end, closed := fishSubstEnd(line, i)
			raw.WriteString(line[i:end])
			val.WriteString(line[i:end])
			subst = true
			if !closed {
				unclosed = true
			}
			i = end
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
		Subst:    subst,
	}, i - start
}

// fishSubstEnd returns the exclusive end of the fish (…) substitution that
// opens at line[start] and whether its closing paren was found. Nested
// parens are balanced; quoted text inside is skipped with fish's rules
// (\\ and \' in single quotes, backslash escapes in double quotes).
func fishSubstEnd(line string, start int) (int, bool) {
	depth := 0
	i := start
	for i < len(line) {
		switch line[i] {
		case '\\':
			i++ // skip the escaped byte
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i + 1, true
			}
		case '\'', '"':
			q := line[i]
			i++
			for i < len(line) && line[i] != q {
				if line[i] == '\\' && i+1 < len(line) {
					i++
				}
				i++
			}
		}
		i++
	}
	return len(line), false
}

// scanCmdSubst reads a $(...) token starting at start (which is '$').
// It tracks parenthesis depth, respecting single/double quotes inside.
func scanCmdSubst(line string, start int, syn Syntax) (Token, int) {
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
				if syn == Fish && line[i] == '\\' && i+1 < len(line) {
					i++
				}
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
