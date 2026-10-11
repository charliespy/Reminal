package updater

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The Host panel renders a release body as plain text with `code` spans — see
// changelog/README.md. Inline markdown is not parsed, so `**bold**` reaches the
// user as literal asterisks. That shipped twice (3.13.2, 3.13.7) before anyone
// noticed, because the mistake is invisible on GitHub, where the same text
// renders correctly. This is the check that would have caught it.
func TestChangelogsCarryNoInlineMarkdown(t *testing.T) {
	for _, dir := range repoChangelogDirs(t) {
		for _, p := range inlineMarkdown(t, dir) {
			t.Error(p)
		}
	}
}

// Every line that is not the title, a bullet, a bullet's indented
// continuation or blank is a subheading, and the Host panel uppercases it
// (changelog/README.md): a sentence there reads as a shouted paragraph. So a
// subheading is a short label, three words at most, letters only.
func TestChangelogSubheadingsAreShortLabels(t *testing.T) {
	for _, dir := range repoChangelogDirs(t) {
		for _, p := range longSubheadings(t, dir) {
			t.Error(p)
		}
	}
}

// Both checks read every changelog directory at the root, whatever follows
// the word: a tree with another (changelog-beta/, say) beside changelog/ has
// both read, with no code naming either. Here, in changelog-beta/, a **bold**
// line and a sentence where a label belongs are caught; its README.md and a
// directory not named changelog* are left alone, and so are labels, bullets
// and their continuations.
func TestChangelogChecksReadEveryChangelogDir(t *testing.T) {
	root := t.TempDir()
	for path, body := range map[string]string{
		"changelog/1.0.0.md":       "# 1.0.0\n\nFixed\n- A plain line.\n",
		"changelog-beta/2.0.0.md":  "# 2.0.0\n\nImproved\n- A **bold** claim.\n",
		"changelog-beta/2.0.1.md":  "# 2.0.1\n\nGood to know\n- A bullet,\n  and its continuation.\nThis line is a whole sentence.\nFor agents (MCP)\n",
		"changelog-beta/README.md": "Rules: no **bold** in the notes. This line is prose.\n",
		"notes/0.1.0.md":           "- Not a changelog, **bold** or not.\nA sentence that is not checked.\n",
	} {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var markup, labels []string
	for _, dir := range changelogDirs(t, root) {
		markup = append(markup, inlineMarkdown(t, dir)...)
		labels = append(labels, longSubheadings(t, dir)...)
	}
	// The bold line, and only it (its asterisks are counted as italics too).
	bold := false
	for _, p := range markup {
		if !strings.HasPrefix(p, "changelog-beta/2.0.0.md:4: ") {
			t.Errorf("markup found outside the bold line: %q", p)
		}
		bold = bold || strings.HasPrefix(p, "changelog-beta/2.0.0.md:4: **bold**")
	}
	if !bold {
		t.Errorf("the bold line in changelog-beta/2.0.0.md was not caught; got %q", markup)
	}
	if len(labels) != 2 || !strings.HasPrefix(labels[0], "changelog-beta/2.0.1.md:6: ") || !strings.HasPrefix(labels[1], "changelog-beta/2.0.1.md:7: ") {
		t.Errorf("want the sentence and the label with brackets in changelog-beta/2.0.1.md (lines 6, 7); got %q", labels)
	}
}

// repoChangelogDirs is this repository's changelog directories.
func repoChangelogDirs(t *testing.T) []string {
	t.Helper()
	dirs := changelogDirs(t, filepath.Join("..", ".."))
	if len(dirs) == 0 {
		t.Skip("no changelog dir")
	}
	return dirs
}

// changelogDirs lists the directories at root whose name starts "changelog".
func changelogDirs(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "changelog") {
			dirs = append(dirs, filepath.Join(root, e.Name()))
		}
	}
	return dirs
}

// eachNoteLine calls fn with every line of every release note in dir, and
// where to say it is: <dir>/<file>:<line>.
func eachNoteLine(t *testing.T, dir string, fn func(where, line string)) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		// README.md documents these rules and is read on GitHub, not in the panel.
		if e.IsDir() || !strings.HasSuffix(name, ".md") || name == "README.md" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			fn(filepath.Base(dir)+"/"+name+":"+itoa(i+1), line)
		}
	}
}

var mdLink = regexp.MustCompile(`\[[^\]]*\]\([^)]*\)`)

// inlineMarkdown is what in dir's notes would render literally in the Host
// panel, one line each.
func inlineMarkdown(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	eachNoteLine(t, dir, func(where, line string) {
		say := func(what string) {
			out = append(out, where+": "+what+" renders literally in the Host panel — "+strings.TrimSpace(line))
		}
		if strings.Contains(line, "**") {
			say("**bold**")
		}
		if mdLink.MatchString(line) {
			say("[a](link)")
		}
		// A lone * is italics; a bullet's leading "- " is fine, and so is a
		// literal * inside a code span, which the panel does render.
		if stripped := stripCode(line); strings.Contains(stripped, "*") {
			say("*italics*")
		}
	})
	return out
}

var shortLabel = regexp.MustCompile(`^[A-Za-z]+( [A-Za-z]+){0,2}$`)

// longSubheadings is each subheading in dir's notes that is not a short
// label.
func longSubheadings(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	eachNoteLine(t, dir, func(where, line string) {
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "- ") ||
			strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			return
		}
		if !shortLabel.MatchString(strings.TrimRight(line, " ")) {
			out = append(out, where+": a subheading is shown in capitals, so keep it a short label (three words, letters only) — "+line)
		}
	})
	return out
}

// stripCode removes `code` spans, whose contents the panel renders verbatim.
func stripCode(line string) string {
	out := make([]rune, 0, len(line))
	in := false
	for _, r := range line {
		if r == '`' {
			in = !in
			continue
		}
		if !in {
			out = append(out, r)
		}
	}
	return string(out)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
