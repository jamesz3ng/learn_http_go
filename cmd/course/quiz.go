package main

import (
	"bufio"
	"crypto/sha1"
	"embed"
	"encoding/hex"
	"fmt"
	"io"
	"math/rand/v2"
	"slices"
	"strings"
)

// Quiz files live in quizzes/NN.txt. The format is line-oriented:
//
//	## The question starts on a line beginning with "## "
//	More question text, which may include ```go code fences```.
//	- a wrong option
//	* a correct option        (several "*" lines = select all that apply)
//	= an accepted typed answer (instead of options)
//	> The explanation, shown after answering. It may span
//	> several lines.
//
// Lines starting with "#" (but not "##") outside a question are comments.

//go:embed quizzes/*.txt
var quizFS embed.FS

type option struct {
	Text    string
	Correct bool
}

type question struct {
	ID          string
	Stage       int
	Prompt      string
	Options     []option
	Answers     []string // accepted typed answers
	Explanation string
}

func (q question) multi() bool {
	n := 0
	for _, o := range q.Options {
		if o.Correct {
			n++
		}
	}
	return n > 1
}

func loadQuiz(n int) ([]question, error) {
	b, err := quizFS.ReadFile(fmt.Sprintf("quizzes/%02d.txt", n))
	if err != nil {
		return nil, fmt.Errorf("no quiz for stage %d", n)
	}
	return parseQuiz(n, string(b))
}

func parseQuiz(stageNum int, src string) ([]question, error) {
	var (
		qs      []question
		cur     *question
		prompt  []string
		explain []string
		inFence bool
	)
	flush := func() error {
		if cur == nil {
			return nil
		}
		cur.Prompt = strings.TrimSpace(strings.Join(prompt, "\n"))
		cur.Explanation = strings.TrimSpace(strings.Join(explain, "\n"))
		if len(cur.Options) == 0 && len(cur.Answers) == 0 {
			return fmt.Errorf("stage %d: question %q has no options or answers", stageNum, firstLine(cur.Prompt))
		}
		if len(cur.Options) > 0 && !slices.ContainsFunc(cur.Options, func(o option) bool { return o.Correct }) {
			return fmt.Errorf("stage %d: question %q has no correct option", stageNum, firstLine(cur.Prompt))
		}
		sum := sha1.Sum([]byte(cur.Prompt))
		cur.ID = fmt.Sprintf("%02d-%s", stageNum, hex.EncodeToString(sum[:4]))
		qs = append(qs, *cur)
		cur, prompt, explain = nil, nil, nil
		return nil
	}

	for _, line := range strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, "## ") && !inFence {
			if err := flush(); err != nil {
				return nil, err
			}
			cur = &question{Stage: stageNum}
			prompt = []string{strings.TrimPrefix(line, "## ")}
			continue
		}
		if cur == nil {
			continue // comments and blank lines before the first question
		}
		// Inside the prompt, code fences may contain anything.
		if len(cur.Options) == 0 && len(cur.Answers) == 0 && len(explain) == 0 {
			if strings.HasPrefix(strings.TrimSpace(line), "```") {
				inFence = !inFence
				prompt = append(prompt, line)
				continue
			}
			if inFence {
				prompt = append(prompt, line)
				continue
			}
		}
		switch {
		case strings.HasPrefix(line, "- "):
			cur.Options = append(cur.Options, option{Text: strings.TrimPrefix(line, "- ")})
		case strings.HasPrefix(line, "* "):
			cur.Options = append(cur.Options, option{Text: strings.TrimPrefix(line, "* "), Correct: true})
		case strings.HasPrefix(line, "= "):
			cur.Answers = append(cur.Answers, strings.TrimPrefix(line, "= "))
		case strings.HasPrefix(line, ">"):
			explain = append(explain, strings.TrimPrefix(strings.TrimPrefix(line, ">"), " "))
		case len(cur.Options) == 0 && len(cur.Answers) == 0:
			prompt = append(prompt, line)
		}
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return qs, nil
}

func firstLine(s string) string {
	l, _, _ := strings.Cut(s, "\n")
	return l
}

// normalize makes typed answers comparable: case, spaces, backticks,
// surrounding quotes and a trailing "()" don't matter.
func normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "`", "")
	s = strings.Trim(s, `"'`)
	s = strings.TrimSuffix(s, "()")
	return strings.Join(strings.Fields(s), " ")
}

// asker runs questions interactively.
type asker struct {
	in  *bufio.Scanner
	out io.Writer
	c   colors
}

type outcome int

const (
	outCorrect outcome = iota
	outWrong
	outSkipped
	outQuit
)

// ask presents q and reports the outcome.
func (a *asker) ask(q question, index, total int) outcome {
	fmt.Fprintf(a.out, "\n%s\n", a.c.bold(fmt.Sprintf("Q%d/%d  (stage %d)", index, total, q.Stage)))
	fmt.Fprintln(a.out, indent(q.Prompt, "  "))

	letters := "abcdefghij"
	var shown []option
	if len(q.Options) > 0 {
		shown = slices.Clone(q.Options)
		rand.Shuffle(len(shown), func(i, j int) { shown[i], shown[j] = shown[j], shown[i] })
		fmt.Fprintln(a.out)
		for i, o := range shown {
			fmt.Fprintf(a.out, "  %c) %s\n", letters[i], o.Text)
		}
		if q.multi() {
			fmt.Fprintln(a.out, a.c.dim("  (select ALL that apply, e.g. a,c)"))
		}
	} else {
		fmt.Fprintln(a.out, a.c.dim("  (type your answer)"))
	}

	var reply string
	for {
		fmt.Fprint(a.out, a.c.cyan("> "))
		if !a.in.Scan() {
			return outQuit
		}
		reply = strings.TrimSpace(a.in.Text())
		if reply == "" {
			continue
		}
		if len(shown) > 0 && !validChoice(reply, len(shown)) {
			fmt.Fprintf(a.out, "%s\n", a.c.dim(fmt.Sprintf("Answer with letters a-%c (e.g. \"b\" or \"a,c\"), s to see the answer, or q to quit.", letters[len(shown)-1])))
			continue
		}
		break
	}
	switch strings.ToLower(reply) {
	case "q", "quit", "exit":
		return outQuit
	case "s", "skip", "?":
		a.reveal(q, shown, letters)
		return outSkipped
	}

	correct := false
	if len(shown) > 0 {
		// Accept "b", "a,c", "a c" and "ac".
		picked := map[int]bool{}
		for _, r := range strings.ToLower(reply) {
			if i := strings.IndexRune(letters[:len(shown)], r); i >= 0 {
				picked[i] = true
			}
		}
		correct = len(picked) > 0
		for i, o := range shown {
			if o.Correct != picked[i] {
				correct = false
			}
		}
	} else {
		for _, ans := range q.Answers {
			if normalize(reply) == normalize(ans) {
				correct = true
			}
		}
	}

	if correct {
		fmt.Fprintln(a.out, a.c.green("✓ Correct."))
		if q.Explanation != "" {
			fmt.Fprintln(a.out, indent(q.Explanation, "  "))
		}
		return outCorrect
	}
	fmt.Fprint(a.out, a.c.red("✗ Not quite. "))
	a.reveal(q, shown, letters)
	return outWrong
}

// validChoice reports whether reply is a command (s, q, ...) or made only of
// option letters, commas and spaces.
func validChoice(reply string, n int) bool {
	switch strings.ToLower(reply) {
	case "q", "quit", "exit", "s", "skip", "?":
		return true
	}
	letters := "abcdefghij"[:n]
	seen := false
	for _, r := range strings.ToLower(reply) {
		switch {
		case strings.ContainsRune(letters, r):
			seen = true
		case r == ',' || r == ' ':
		default:
			return false
		}
	}
	return seen
}

func (a *asker) reveal(q question, shown []option, letters string) {
	if len(shown) > 0 {
		var right []string
		for i, o := range shown {
			if o.Correct {
				right = append(right, fmt.Sprintf("%c) %s", letters[i], o.Text))
			}
		}
		fmt.Fprintf(a.out, "Answer: %s\n", strings.Join(right, "; "))
	} else {
		fmt.Fprintf(a.out, "Answer: %s\n", q.Answers[0])
	}
	if q.Explanation != "" {
		fmt.Fprintln(a.out, indent(q.Explanation, "  "))
	}
}

func indent(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = prefix + l
		}
	}
	return strings.Join(lines, "\n")
}
