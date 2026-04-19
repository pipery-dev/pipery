package pipery

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chzyer/readline"
)

var shellBuiltins = []string{
	"cd",
	"pwd",
	"export",
	"unset",
	"exit",
	"quit",
}

type shellAutoCompleter struct {
	session *session
}

func newShellAutoCompleter(s *session) readline.AutoCompleter {
	return &shellAutoCompleter{session: s}
}

func (c *shellAutoCompleter) Do(line []rune, pos int) ([][]rune, int) {
	if pos < 0 || pos > len(line) {
		pos = len(line)
	}

	prefix := string(line[:pos])
	token, commandPosition := completionContext(prefix)

	var suggestions []string
	if shouldCompletePath(token, commandPosition) {
		suggestions = pathCompletionSuffixes(c.session.cwd, token, c.session.env["HOME"])
	} else {
		suggestions = commandCompletionSuffixes(token, c.session.env, shellBuiltins)
	}

	completions := make([][]rune, 0, len(suggestions))
	for _, suggestion := range suggestions {
		completions = append(completions, []rune(suggestion))
	}

	return completions, len([]rune(token))
}

func completionContext(line string) (string, bool) {
	inSingle := false
	inDouble := false
	escaped := false
	commandPosition := true
	currentToken := ""

	for _, r := range line {
		if escaped {
			currentToken += string(r)
			escaped = false
			commandPosition = false
			continue
		}

		switch r {
		case '\\':
			escaped = true
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
			currentToken += string(r)
			commandPosition = false
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
			currentToken += string(r)
			commandPosition = false
		case ' ', '\t':
			if inSingle || inDouble {
				currentToken += string(r)
				continue
			}
			if currentToken != "" {
				commandPosition = false
				currentToken = ""
			}
		case '|', ';', '&':
			if inSingle || inDouble {
				currentToken += string(r)
				continue
			}
			currentToken = ""
			commandPosition = true
		default:
			currentToken += string(r)
		}
	}

	return currentToken, commandPosition
}

func shouldCompletePath(token string, commandPosition bool) bool {
	if token == "" {
		return !commandPosition
	}

	if strings.HasPrefix(token, "/") ||
		strings.HasPrefix(token, "./") ||
		strings.HasPrefix(token, "../") ||
		strings.HasPrefix(token, "~/") {
		return true
	}

	if strings.Contains(token, "/") {
		return true
	}

	return !commandPosition
}

func commandCompletionSuffixes(fragment string, env map[string]string, builtins []string) []string {
	seen := make(map[string]struct{})
	candidates := make([]string, 0, len(builtins))

	for _, builtin := range builtins {
		if !strings.HasPrefix(builtin, fragment) {
			continue
		}
		if _, ok := seen[builtin]; ok {
			continue
		}
		seen[builtin] = struct{}{}
		candidates = append(candidates, strings.TrimPrefix(builtin, fragment))
	}

	for _, executable := range executablesOnPath(env["PATH"]) {
		if !strings.HasPrefix(executable, fragment) {
			continue
		}
		if _, ok := seen[executable]; ok {
			continue
		}
		seen[executable] = struct{}{}
		candidates = append(candidates, strings.TrimPrefix(executable, fragment))
	}

	sort.Strings(candidates)
	return candidates
}

func executablesOnPath(pathValue string) []string {
	seen := map[string]struct{}{}
	entries := []string{}

	for _, dir := range filepath.SplitList(pathValue) {
		if dir == "" {
			continue
		}

		dirEntries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}

		for _, entry := range dirEntries {
			name := entry.Name()
			if _, ok := seen[name]; ok {
				continue
			}

			info, err := entry.Info()
			if err != nil || info.IsDir() {
				continue
			}

			if info.Mode()&0o111 == 0 {
				continue
			}

			seen[name] = struct{}{}
			entries = append(entries, name)
		}
	}

	sort.Strings(entries)
	return entries
}

func pathCompletionSuffixes(cwd, fragment, home string) []string {
	searchDir, prefix, visiblePrefix := resolveCompletionPath(cwd, fragment, home)

	entries, err := os.ReadDir(searchDir)
	if err != nil {
		return nil
	}

	suffixes := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) {
			continue
		}

		candidate := visiblePrefix + name
		if entry.IsDir() {
			candidate += "/"
		}

		suffixes = append(suffixes, strings.TrimPrefix(candidate, fragment))
	}

	sort.Strings(suffixes)
	return suffixes
}

func resolveCompletionPath(cwd, fragment, home string) (string, string, string) {
	resolved := fragment
	if strings.HasPrefix(fragment, "~/") && home != "" {
		resolved = filepath.Join(home, strings.TrimPrefix(fragment, "~/"))
	}

	if fragment == "" {
		return cwd, "", ""
	}

	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(cwd, resolved)
	}

	if strings.HasSuffix(fragment, "/") {
		return resolved, "", fragment
	}

	dirPart := filepath.Dir(resolved)
	basePart := filepath.Base(resolved)
	visibleDir := ""
	if strings.Contains(fragment, "/") {
		visibleDir = fragment[:len(fragment)-len(basePart)]
	}

	return dirPart, basePart, visibleDir
}
