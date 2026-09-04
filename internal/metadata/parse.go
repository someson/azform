package metadata

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	entryLineRE  = regexp.MustCompile(`^ {4}(\S.*?)\s+:\s*(.*)$`)
	integerRE    = regexp.MustCompile(`^-?\d+$`)
	spaceRunRE   = regexp.MustCompile(`\s+`)
	spacePunctRE = regexp.MustCompile(`\s+([.,;:])`)
)

// Parse detects the kind of an `az ... --help` page and parses it. The input
// is raw stdout; the parser never invokes az itself.
func Parse(raw string) (*Document, error) {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")
	lines := strings.Split(raw, "\n")

	for _, line := range lines {
		switch strings.TrimSpace(line) {
		case "Command":
			return parseCommand(lines)
		case "Group":
			return parseGroup(lines)
		}
	}
	return nil, errors.New("metadata: help page has neither Command nor Group header")
}

type parseState int

const (
	stateBeforeHeader parseState = iota
	stateHeader
	stateArguments
	stateExamples
)

func parseCommand(lines []string) (*Document, error) {
	cmd := &Command{Parameters: []Parameter{}}
	health := ParseHealth{SectionsOK: true}
	state := stateBeforeHeader
	section := ""
	var current *Parameter

	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		indent := leadingSpaces(line)

		if indent == 0 {
			switch {
			case trimmed == "Command":
				state = stateHeader
				current = nil
				continue
			case trimmed == "Examples":
				state = stateExamples
				current = nil
				continue
			case isArgumentSection(trimmed):
				state = stateArguments
				section = trimmed
				current = nil
				continue
			case trimmed == "Group" || trimmed == "Subgroups:" || trimmed == "Commands:":
				// A command page must not turn into a group page halfway through.
				health.SectionsOK = false
				current = nil
				continue
			default:
				// Free-form top-level lines are common inside Examples. Elsewhere
				// they indicate a section shape this parser does not know.
				if state != stateExamples && state != stateBeforeHeader && state != stateHeader {
					health.SectionsOK = false
				}
				current = nil
				continue
			}
		}

		switch state {
		case stateHeader:
			if cmd.Command == "" {
				path, summary, ok := parseHelpTitle(trimmed)
				if !ok {
					if indent == 4 {
						health.UnparsedLines++
					}
					continue
				}
				cmd.Command = path
				cmd.Summary = summary
				continue
			}
			cmd.Summary = joinText(cmd.Summary, trimmed)

		case stateArguments:
			if indent == 4 {
				p, ok := parseParameterLine(line, section)
				if !ok {
					health.UnparsedLines++
					current = nil
					continue
				}
				cmd.Parameters = append(cmd.Parameters, p)
				current = &cmd.Parameters[len(cmd.Parameters)-1]
				continue
			}
			if indent > 4 && current != nil {
				current.Help = joinText(current.Help, trimmed)
				continue
			}
			health.UnparsedLines++
		}
	}

	if cmd.Command == "" {
		return nil, errors.New("metadata: command help page has no az title")
	}
	for i := range cmd.Parameters {
		finalizeParameter(&cmd.Parameters[i])
	}
	health.Params = len(cmd.Parameters)
	if len(cmd.Parameters) == 0 {
		health.SectionsOK = false
	}

	return &Document{Kind: DocumentKindCommand, Command: cmd, ParseHealth: health}, nil
}

type groupState int

const (
	groupStateBeforeHeader groupState = iota
	groupStateHeader
	groupStateSubgroups
	groupStateCommands
	groupStateExamples
)

func parseGroup(lines []string) (*Document, error) {
	group := &Group{Subgroups: []NavigationItem{}, Commands: []NavigationItem{}}
	health := ParseHealth{SectionsOK: true}
	state := groupStateBeforeHeader
	var current *NavigationItem

	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		indent := leadingSpaces(line)

		if indent == 0 {
			switch trimmed {
			case "Group":
				state = groupStateHeader
				current = nil
				continue
			case "Subgroups:":
				state = groupStateSubgroups
				current = nil
				continue
			case "Commands:":
				state = groupStateCommands
				current = nil
				continue
			case "Examples":
				state = groupStateExamples
				current = nil
				continue
			default:
				if state != groupStateExamples && state != groupStateBeforeHeader && state != groupStateHeader {
					health.SectionsOK = false
				}
				current = nil
				continue
			}
		}

		switch state {
		case groupStateHeader:
			if group.Group == "" {
				path, summary, ok := parseHelpTitle(trimmed)
				if !ok {
					if indent == 4 {
						health.UnparsedLines++
					}
					continue
				}
				group.Group = path
				group.Summary = summary
				continue
			}
			group.Summary = joinText(group.Summary, trimmed)

		case groupStateSubgroups, groupStateCommands:
			if indent == 4 {
				item, ok := parseNavigationLine(line)
				if !ok {
					health.UnparsedLines++
					current = nil
					continue
				}
				if state == groupStateSubgroups {
					group.Subgroups = append(group.Subgroups, item)
					current = &group.Subgroups[len(group.Subgroups)-1]
				} else {
					group.Commands = append(group.Commands, item)
					current = &group.Commands[len(group.Commands)-1]
				}
				continue
			}
			if indent > 4 && current != nil {
				current.Summary = joinText(current.Summary, trimmed)
				continue
			}
			health.UnparsedLines++
		}
	}

	if group.Group == "" {
		return nil, errors.New("metadata: group help page has no az title")
	}
	health.Params = len(group.Subgroups) + len(group.Commands)
	if health.Params == 0 {
		health.SectionsOK = false
	}

	return &Document{Kind: DocumentKindGroup, Group: group, ParseHealth: health}, nil
}

func isArgumentSection(s string) bool {
	return s == "Arguments" || s == "Positional" || strings.HasSuffix(s, " Arguments")
}

func parseHelpTitle(line string) (path, summary string, ok bool) {
	if !strings.HasPrefix(line, "az ") {
		return "", "", false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(line, "az "))
	idx := strings.Index(rest, " : ")
	if idx < 0 {
		idx = strings.Index(rest, ":")
	}
	if idx < 0 {
		return "", "", false
	}
	path = strings.TrimSpace(rest[:idx])
	summary = strings.TrimSpace(rest[idx+1:])
	summary = strings.TrimPrefix(summary, ":")
	summary = strings.TrimSpace(summary)
	if path == "" {
		return "", "", false
	}
	return path, summary, true
}

func parseParameterLine(line, section string) (Parameter, bool) {
	m := entryLineRE.FindStringSubmatch(line)
	if m == nil {
		return Parameter{}, false
	}

	left := strings.TrimSpace(m[1])
	desc := strings.TrimSpace(m[2])
	required := false
	var names []string
	for _, token := range strings.Fields(left) {
		switch token {
		case "[Required]":
			required = true
			continue
		case "[Preview]", "[Deprecated]", "[Experimental]":
			// The cache schema intentionally does not persist status badges yet.
			continue
		}
		names = append(names, token)
	}
	if len(names) == 0 {
		return Parameter{}, false
	}

	canonical := ""
	for _, name := range names {
		if strings.HasPrefix(name, "--") {
			canonical = name
			break
		}
	}
	if canonical == "" {
		canonical = names[0]
	}

	aliases := make([]string, 0, len(names)-1)
	for _, name := range names {
		if name != canonical {
			aliases = append(aliases, name)
		}
	}

	return Parameter{
		Name:       canonical,
		Aliases:    aliases,
		Required:   required,
		Group:      parameterGroup(section, required),
		Global:     section == "Global Arguments" || section == "Global Policy Arguments",
		TakesValue: true,
		ValueKind:  ValueKindString,
		Help:       desc,
	}, true
}

func parseNavigationLine(line string) (NavigationItem, bool) {
	m := entryLineRE.FindStringSubmatch(line)
	if m == nil {
		return NavigationItem{}, false
	}
	item := NavigationItem{Summary: strings.TrimSpace(m[2])}
	for _, token := range strings.Fields(strings.TrimSpace(m[1])) {
		switch token {
		case "[Preview]":
			item.Preview = true
		case "[Deprecated]":
			item.Deprecated = true
		case "[Experimental]":
			item.Experimental = true
		default:
			if item.Name == "" {
				item.Name = token
			}
		}
	}
	if item.Name == "" {
		return NavigationItem{}, false
	}
	return item, true
}

func parameterGroup(section string, required bool) string {
	if section == "Arguments" {
		if required {
			return "Required Parameters"
		}
		return "Optional Parameters"
	}
	return section
}

func finalizeParameter(p *Parameter) {
	help := normalizeText(p.Help)
	var choices []string
	help, choices = extractChoices(help)
	var valuesFrom *string
	help, valuesFrom = extractValuesFrom(help)
	var defaultValue *string
	help, defaultValue = extractDefault(help)
	help = cleanupHelp(help)

	p.Help = help
	p.Choices = choices
	p.ValuesFrom = valuesFrom
	p.Default = defaultValue
	p.ValueKind = inferValueKind(p, choices, defaultValue)
	p.TakesValue = takesValue(p)
}

func inferValueKind(p *Parameter, choices []string, defaultValue *string) ValueKind {
	if len(choices) > 0 {
		if isBoolChoiceSet(choices) {
			return ValueKindBool
		}
		return ValueKindEnum
	}

	lowerName := strings.ToLower(p.Name)
	lowerHelp := strings.ToLower(p.Help)
	if p.Group == "Positional" {
		return ValueKindList
	}
	if strings.Contains(lowerHelp, "@{") || strings.Contains(lowerHelp, "@<") ||
		strings.Contains(lowerHelp, "@file") || strings.Contains(lowerHelp, "path to") ||
		strings.Contains(lowerHelp, "file path") || strings.HasSuffix(lowerName, "-file") ||
		strings.HasSuffix(lowerName, "-path") || lowerName == "--custom-data" {
		return ValueKindPath
	}
	if strings.Contains(lowerHelp, "key=value") || strings.Contains(lowerHelp, "key[=value]") ||
		strings.Contains(lowerHelp, "key:value") {
		return ValueKindKeyValue
	}
	if strings.Contains(lowerHelp, "space-separated") || strings.Contains(lowerHelp, "comma-separated") ||
		strings.Contains(lowerHelp, "one or more") || strings.Contains(lowerHelp, "list of") {
		return ValueKindList
	}
	if defaultValue != nil && integerRE.MatchString(*defaultValue) {
		return ValueKindInt
	}
	if strings.HasPrefix(lowerHelp, "number of ") || strings.HasPrefix(lowerHelp, "the number of ") {
		return ValueKindInt
	}
	if looksLikeSwitch(p.Name, p.Help) {
		return ValueKindBool
	}
	return ValueKindString
}

func takesValue(p *Parameter) bool {
	return p.ValueKind != ValueKindBool || len(p.Choices) != 0 || p.Default != nil || !looksLikeSwitch(p.Name, p.Help)
}

var boolChoiceSynonyms = map[string]struct{}{
	"0": {}, "1": {}, "f": {}, "false": {}, "n": {}, "no": {},
	"t": {}, "true": {}, "y": {}, "yes": {},
}

func isBoolChoiceSet(choices []string) bool {
	if len(choices) == 0 {
		return false
	}
	for _, choice := range choices {
		if _, ok := boolChoiceSynonyms[strings.ToLower(choice)]; !ok {
			return false
		}
	}
	return true
}

func looksLikeSwitch(name, help string) bool {
	switch name {
	case "--debug", "--help", "--only-show-errors", "--verbose", "--yes",
		"--no-wait", "--service-principal", "--use-device-code", "--identity",
		"--assign-identity", "--generate-ssh-keys", "--validate", "--force",
		"--skip-subscription-discovery", "--skip-authorization-header":
		return true
	}
	if strings.HasPrefix(name, "--no-") {
		return true
	}

	lower := strings.ToLower(strings.TrimSpace(help))
	switch {
	case strings.HasPrefix(lower, "do not "),
		strings.HasPrefix(lower, "show this help"),
		strings.HasPrefix(lower, "increase logging"),
		strings.HasPrefix(lower, "only show errors"),
		strings.HasPrefix(lower, "enable "),
		strings.HasPrefix(lower, "disable "),
		strings.HasPrefix(lower, "skip "),
		strings.HasPrefix(lower, "generate "),
		strings.HasPrefix(lower, "log in with "),
		strings.HasPrefix(lower, "log in using "),
		strings.HasPrefix(lower, "use device code"),
		strings.HasPrefix(lower, "support accessing tenants"),
		strings.HasPrefix(lower, "force "),
		strings.HasPrefix(lower, "block "),
		strings.HasPrefix(lower, "wait "):
		return true
	default:
		return false
	}
}

func extractChoices(help string) (string, []string) {
	var all []string
	for {
		value, start, end, ok := findMarkerSegment(help, "Allowed values:", 0)
		if !ok {
			break
		}
		for _, choice := range strings.Split(value, ",") {
			choice = normalizeChoice(choice)
			if choice != "" && !containsString(all, choice) {
				all = append(all, choice)
			}
		}
		help = help[:start] + help[end:]
	}
	if len(all) == 0 {
		return help, nil
	}
	return help, all
}

func extractValuesFrom(help string) (string, *string) {
	return extractScalarMarker(help, "Values from:")
}

func extractDefault(help string) (string, *string) {
	return extractScalarMarker(help, "Default:")
}

func extractScalarMarker(help, marker string) (string, *string) {
	var found *string
	for {
		value, start, end, ok := findMarkerSegment(help, marker, 0)
		if !ok {
			break
		}
		if found == nil {
			v := strings.TrimSpace(value)
			if marker == "Values from:" {
				v = strings.Trim(v, "`")
			}
			if marker == "Default:" && v == "" {
				v = "."
			}
			found = &v
		}
		help = help[:start] + help[end:]
	}
	return help, found
}

// findMarkerSegment finds a structured sentence marker and returns its value
// plus the exact span to remove from Help. The terminating period is consumed,
// while periods inside values such as "1.2" are preserved.
func findMarkerSegment(s, marker string, from int) (value string, start, end int, ok bool) {
	rel := strings.Index(s[from:], marker)
	if rel < 0 {
		return "", 0, 0, false
	}
	start = from + rel
	i := start + len(marker)
	for i < len(s) && s[i] == ' ' {
		i++
	}

	valueEnd := len(s)
	segmentEnd := len(s)
	for j := i; j < len(s); j++ {
		if s[j] != '.' {
			continue
		}
		if j+1 == len(s) {
			valueEnd = j
			segmentEnd = j + 1
			break
		}
		if s[j+1] == ' ' {
			k := j + 1
			for k < len(s) && s[k] == ' ' {
				k++
			}
			if k >= len(s) || startsMarker(s[k:]) || startsUpper(s[k:]) {
				valueEnd = j
				segmentEnd = j + 1
				break
			}
		}
	}
	if valueEnd == len(s) {
		next := len(s)
		for _, other := range []string{"Allowed values:", "Values from:", "Default:"} {
			if other == marker {
				continue
			}
			if idx := strings.Index(s[i:], other); idx >= 0 && i+idx < next {
				next = i + idx
			}
		}
		valueEnd = next
		segmentEnd = next
	}

	return strings.TrimSpace(s[i:valueEnd]), start, segmentEnd, true
}

func startsMarker(s string) bool {
	return strings.HasPrefix(s, "Allowed values:") || strings.HasPrefix(s, "Values from:") || strings.HasPrefix(s, "Default:")
}

func startsUpper(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return unicode.IsUpper(r)
}

func normalizeChoice(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, ".")
	if len(s) >= 2 {
		if (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"') {
			s = s[1 : len(s)-1]
		}
	}
	return strings.TrimSpace(s)
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func normalizeText(s string) string {
	return strings.TrimSpace(spaceRunRE.ReplaceAllString(s, " "))
}

func cleanupHelp(s string) string {
	s = normalizeText(s)
	s = spacePunctRE.ReplaceAllString(s, "$1")
	return strings.TrimSpace(s)
}

func joinText(a, b string) string {
	if a == "" {
		return b
	}
	return a + " " + b
}

func leadingSpaces(s string) int {
	count := 0
	for count < len(s) && s[count] == ' ' {
		count++
	}
	return count
}

// SortParameters is used by tests and diagnostics when a stable ordering by
// name is more useful than the original help ordering.
func SortParameters(params []Parameter) {
	sort.SliceStable(params, func(i, j int) bool { return params[i].Name < params[j].Name })
}

// ValidateDocument performs a cheap structural check used by tests and the
// resolver before a parsed page is written into the cache.
func ValidateDocument(doc *Document) error {
	if doc == nil {
		return errors.New("metadata: nil document")
	}
	switch doc.Kind {
	case DocumentKindCommand:
		if doc.Command == nil || doc.Command.Command == "" {
			return errors.New("metadata: command document is incomplete")
		}
		for _, p := range doc.Command.Parameters {
			if p.Name == "" {
				return fmt.Errorf("metadata: parameter with empty name in %q", doc.Command.Command)
			}
		}
	case DocumentKindGroup:
		if doc.Group == nil || doc.Group.Group == "" {
			return errors.New("metadata: group document is incomplete")
		}
	default:
		return fmt.Errorf("metadata: unknown document kind %q", doc.Kind)
	}
	return nil
}
