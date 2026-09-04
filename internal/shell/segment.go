package shell

import "strings"

// RawBuffer is the result of locating and extracting the target az command
// from the shell buffer. It contains everything needed to call MatchParams
// once metadata is available.
type RawBuffer struct {
	CommandPath string  // "group create" — tokens between "az" and first "-" flag
	FlagTokens  []Token // flag and value tokens following the command path
	Prefix      string  // text in the original line before the az command (verbatim)
	Suffix      string  // text in the original line after the az command (verbatim)
	Inline      bool    // true when az lives inside $() or ``
	CursorByte  int     // cursor position in the original line; -1 for inline segments
}

// azSegment is an internal representation of one located az command.
type azSegment struct {
	commandPath string
	flagTokens  []Token
	prefix      string
	suffix      string
	inline      bool
	outerStart  int // byte position of the az-containing context in the original line
	outerEnd    int // exclusive byte position
}

// ParseRaw locates the target az command in line, extracts its command path
// and raw flag tokens, and returns everything needed for MatchParams.
//
// It returns (RawBuffer{}, false) when no az segment exists in the line.
// When multiple az segments exist, the one containing cursor is selected;
// if cursor is outside all segments, the first segment is selected.
func ParseRaw(line string, cursor int) (RawBuffer, bool) {
	segs := findSegments(line)
	if len(segs) == 0 {
		return RawBuffer{}, false
	}
	target := selectTarget(segs, cursor)
	cb := cursor
	if target.inline {
		cb = -1
	}
	return RawBuffer{
		CommandPath: target.commandPath,
		FlagTokens:  target.flagTokens,
		Prefix:      target.prefix,
		Suffix:      target.suffix,
		Inline:      target.inline,
		CursorByte:  cb,
	}, true
}

// findSegments searches line (and recursively inside $() / “ tokens) for
// all az commands. Segments are returned in document order.
func findSegments(line string) []azSegment {
	tokens := Tokenize(line)
	var segs []azSegment
	i := 0
	for i < len(tokens) {
		tok := tokens[i]
		if tok.Kind == TokWord && tok.Value == "az" {
			seg, consumed := extractSegment(tokens, i, line)
			segs = append(segs, seg)
			i += consumed
			continue
		}
		if tok.Kind == TokCmdSubst {
			inner := tok.Inner
			isBacktick := tok.Raw != "" && tok.Raw[0] == '`'
			innerSegs := findSegments(inner)
			for _, s := range innerSegs {
				var prefix, suffix string
				if isBacktick {
					prefix = line[:tok.Start] + "`" + s.prefix
					suffix = s.suffix + "`" + line[tok.End:]
				} else {
					prefix = line[:tok.Start] + "$(" + s.prefix
					suffix = s.suffix + ")" + line[tok.End:]
				}
				segs = append(segs, azSegment{
					commandPath: s.commandPath,
					flagTokens:  s.flagTokens,
					prefix:      prefix,
					suffix:      suffix,
					inline:      true,
					outerStart:  tok.Start,
					outerEnd:    tok.End,
				})
			}
		}
		i++
	}
	return segs
}

// extractSegment builds an azSegment from the token list starting at azIdx
// (which must be the "az" TokWord). It returns the segment and the number of
// tokens consumed (including the "az" token itself). Callers that discover
// an inline segment inside $() or “ set inline=true on the returned value.
func extractSegment(tokens []Token, azIdx int, line string) (azSegment, int) {
	i := azIdx + 1

	// Command path: TokWord tokens that do not start with '-'
	var cmdParts []string
	for i < len(tokens) && tokens[i].Kind == TokWord && !strings.HasPrefix(tokens[i].Value, "-") {
		cmdParts = append(cmdParts, tokens[i].Value)
		i++
	}

	// Flag tokens: everything until an operator (or end)
	var flagTokens []Token
	for i < len(tokens) && tokens[i].Kind != TokOp {
		flagTokens = append(flagTokens, tokens[i])
		i++
	}

	azStart := tokens[azIdx].Start
	azEnd := tokens[azIdx].End
	if i > azIdx+1 {
		azEnd = tokens[i-1].End
	}

	return azSegment{
		commandPath: strings.Join(cmdParts, " "),
		flagTokens:  flagTokens,
		prefix:      line[:azStart],
		suffix:      line[azEnd:],
		outerStart:  azStart,
		outerEnd:    azEnd,
	}, i - azIdx
}

// selectTarget picks the az segment that contains cursor, or the first segment
// when cursor is outside all segments.
func selectTarget(segs []azSegment, cursor int) *azSegment {
	for i := range segs {
		if cursor >= segs[i].outerStart && cursor < segs[i].outerEnd {
			return &segs[i]
		}
	}
	return &segs[0]
}
