// Command course is the companion tool for this course: it runs the stage
// tests, tracks your progress, and quizzes you.
//
//	go run ./cmd/course            # where am I? what next?
//	go run ./cmd/course test       # test every stage up to your current one
//	go run ./cmd/course test 7     # test only stage 7 (also: 3-7, all)
//	go run ./cmd/course quiz 7     # take stage 7's quiz
//	go run ./cmd/course review     # spaced-repetition review of past quizzes
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

const usage = `course — the companion tool for "Learn net/http by building"

Usage:
  go run ./cmd/course [status]          show your progress and what to do next
  go run ./cmd/course test [STAGES]     run stage tests; STAGES is N, N-M or "all"
                                        (default: stage 1 up to your current stage)
        -race                           also run Go's race detector (needs cgo)
        -v                              show output from passing tests too
  go run ./cmd/course quiz [N]          take the quiz for stage N
  go run ./cmd/course review [-n 10]    review quiz questions that are due (spaced repetition)
        -all                            review even if nothing is due
  go run ./cmd/course reset -yes        forget all progress
`

func main() {
	root, err := findRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "course:", err)
		os.Exit(1)
	}
	prog, err := loadProgress(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "course: reading progress:", err)
		os.Exit(1)
	}
	c := newColors()

	args := os.Args[1:]
	cmd := "status"
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}
	switch cmd {
	case "status":
		err = cmdStatus(prog, c)
	case "test":
		err = cmdTest(root, prog, c, args)
	case "quiz":
		err = cmdQuiz(prog, c, args)
	case "review":
		err = cmdReview(prog, c, args)
	case "reset":
		err = cmdReset(prog, args)
	case "help", "-h", "-help", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "course: unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "course:", err)
		os.Exit(1)
	}
}

// findRoot walks up from the working directory to this module's go.mod.
func findRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		b, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil && strings.Contains(string(b), "module github.com/jamesz3ng/learn_http_go") {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("run this from inside the learn_http_go repository")
		}
		dir = parent
	}
}

// --- status

func cmdStatus(p *progress, c colors) error {
	cur := p.current()
	fmt.Println(c.bold("Learn net/http by building"))
	part := ""
	for _, s := range stages {
		if s.Part != part {
			part = s.Part
			fmt.Printf("\n  %s\n", c.dim("Part "+part))
		}
		mark := "  "
		switch {
		case s.Num == 0:
			mark = c.dim("· ")
		case !p.Passed[s.Num].IsZero():
			mark = c.green("✓ ")
		case s.Num == cur:
			mark = c.yellow("→ ")
		default:
			mark = c.dim("○ ")
		}
		quiz := ""
		if r, ok := p.Quizzes[s.Num]; ok {
			quiz = c.dim(fmt.Sprintf("  quiz %d/%d", r.Score, r.Total))
		}
		line := fmt.Sprintf("%s%2d  %-44s %s", mark, s.Num, s.Title, c.dim(s.Project))
		if s.Num == cur {
			line = c.bold(line)
		}
		fmt.Println("  " + line + quiz)
	}
	fmt.Println()

	if due := len(p.dueCards(time.Now())); due > 0 {
		fmt.Printf("  %s %d quiz question(s) are due for review: go run ./cmd/course review\n\n", c.cyan("↻"), due)
	}
	if cur > lastStage {
		fmt.Println(c.green("  You've passed every stage. Congratulations! See docs/beyond.md for where to go next."))
		return nil
	}
	s, _ := stageByNum(cur)
	if cur == 1 && len(p.Passed) == 0 {
		fmt.Printf("  New here? Start with %s\n", c.cyan(stages[0].readme()))
	}
	fmt.Printf("  Next: %s — %s\n", c.bold(s.label()), s.Title)
	fmt.Printf("    1. read     %s\n", c.cyan(s.readme()))
	fmt.Printf("    2. test     %s\n", c.cyan("go run ./cmd/course test"))
	fmt.Printf("    3. quiz     %s\n", c.cyan(fmt.Sprintf("go run ./cmd/course quiz %d", cur)))
	return nil
}

// --- test

func cmdTest(root string, p *progress, c colors, args []string) error {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	race := fs.Bool("race", false, "run with the race detector")
	verbose := fs.Bool("v", false, "show output of passing tests")
	if err := fs.Parse(reorderFlags(args)); err != nil {
		return err
	}
	from, to := 1, min(p.current(), lastStage)
	if fs.NArg() > 0 {
		var err error
		from, to, err = parseRange(fs.Arg(0))
		if err != nil {
			return err
		}
	}
	results, err := runStageTests(root, from, to, *race, *verbose, c)
	if err != nil {
		return err
	}

	allPassed := true
	firstFail := 0
	for n := from; n <= to; n++ {
		r := results[n]
		if r.passed() {
			if p.Passed[n].IsZero() {
				p.Passed[n] = time.Now()
			}
			continue
		}
		allPassed = false
		if firstFail == 0 {
			firstFail = n
		}
	}
	if err := p.save(); err != nil {
		return err
	}

	fmt.Println()
	if !allPassed {
		s, _ := stageByNum(firstFail)
		fmt.Printf("%s %s isn't passing yet. Re-read the task in %s — the hints there are progressive.\n",
			c.red("✗"), s.label(), c.cyan(s.readme()))
		os.Exit(1)
	}
	if from == to {
		s, _ := stageByNum(to)
		fmt.Printf("%s %s passed!\n", c.green("✓"), s.label())
	} else {
		fmt.Printf("%s Stages %d–%d passed!\n", c.green("✓"), from, to)
	}
	if _, took := p.Quizzes[to]; !took {
		fmt.Printf("  Lock it in:  %s\n", c.cyan(fmt.Sprintf("go run ./cmd/course quiz %d", to)))
	}
	if next := p.current(); next <= lastStage {
		s, _ := stageByNum(next)
		fmt.Printf("  Then:        %s (%s)\n", c.cyan(s.readme()), s.Title)
	} else {
		fmt.Println(c.green("  That was the last stage. Well done!"))
	}
	return nil
}

// reorderFlags moves flags before positional arguments, so both
// "test 3 -race" and "test -race 3" work.
func reorderFlags(args []string) []string {
	var flags, rest []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
		} else {
			rest = append(rest, a)
		}
	}
	return append(flags, rest...)
}

func parseRange(s string) (int, int, error) {
	if s == "all" {
		return 1, lastStage, nil
	}
	a, b, isRange := strings.Cut(s, "-")
	from, err := strconv.Atoi(a)
	if err != nil {
		return 0, 0, fmt.Errorf("bad stage %q (want N, N-M or all)", s)
	}
	to := from
	if isRange {
		if to, err = strconv.Atoi(b); err != nil {
			return 0, 0, fmt.Errorf("bad stage range %q", s)
		}
	}
	if from < 1 || to > lastStage || from > to {
		return 0, 0, fmt.Errorf("stages run from 1 to %d", lastStage)
	}
	return from, to, nil
}

// --- quiz

func cmdQuiz(p *progress, c colors, args []string) error {
	n := 0
	if len(args) > 0 {
		var err error
		if n, err = strconv.Atoi(args[0]); err != nil {
			return fmt.Errorf("bad stage number %q", args[0])
		}
	} else {
		// Default: the first passed stage whose quiz you haven't taken,
		// otherwise the stage you're working on.
		n = min(p.current(), lastStage)
		for i := 0; i <= lastStage; i++ {
			_, passed := p.Passed[i]
			_, took := p.Quizzes[i]
			if (passed || i == 0) && !took {
				n = i
				break
			}
		}
	}
	s, ok := stageByNum(n)
	if !ok {
		return fmt.Errorf("no stage %d", n)
	}
	qs, err := loadQuiz(n)
	if err != nil {
		return err
	}
	rand.Shuffle(len(qs), func(i, j int) { qs[i], qs[j] = qs[j], qs[i] })

	fmt.Printf("%s — %s quiz, %d questions\n", c.bold(s.label()), s.Title, len(qs))
	fmt.Println(c.dim("Answer with a letter (several for \"select all\"), or type the answer. s = show answer, q = quit."))
	fmt.Println(c.dim("Answer from memory before peeking at your notes: the effort of recalling is what makes it stick."))

	a := &asker{in: bufio.NewScanner(os.Stdin), out: os.Stdout, c: c}
	score, asked := 0, 0
	var missed []question
	now := time.Now()
	for i, q := range qs {
		res := a.ask(q, i+1, len(qs))
		if res == outQuit {
			break
		}
		asked++
		cd := p.Cards[q.ID]
		if cd == nil {
			cd = &card{Stage: n, Box: 1}
			p.Cards[q.ID] = cd
		}
		cd.answer(res == outCorrect, now)
		if res == outCorrect {
			score++
		} else {
			missed = append(missed, q)
		}
	}
	if asked == 0 {
		return nil
	}
	if asked == len(qs) {
		p.Quizzes[n] = quizResult{Score: score, Total: len(qs), Taken: now}
	}
	if err := p.save(); err != nil {
		return err
	}

	fmt.Printf("\n%s %d/%d\n", c.bold("Score:"), score, asked)
	switch {
	case asked < len(qs):
		fmt.Println("Quiz stopped early; the questions you answered are scheduled for review.")
	case score == len(qs):
		fmt.Println(c.green("Perfect. These questions will come back in a day, then less and less often."))
	default:
		fmt.Println("Missed questions come back in your next review. Worth re-reading:")
		for _, q := range missed {
			fmt.Printf("  · %s\n", firstLine(q.Prompt))
		}
		fmt.Printf("  in %s\n", c.cyan(s.readme()))
	}
	return nil
}

// --- review

func cmdReview(p *progress, c colors, args []string) error {
	fs := flag.NewFlagSet("review", flag.ContinueOnError)
	limit := fs.Int("n", 10, "maximum number of questions")
	all := fs.Bool("all", false, "review even if nothing is due")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Load every question we have a card for.
	byID := map[string]question{}
	for n := 0; n <= lastStage; n++ {
		qs, err := loadQuiz(n)
		if err != nil {
			continue
		}
		for _, q := range qs {
			if _, ok := p.Cards[q.ID]; ok {
				byID[q.ID] = q
			}
		}
	}
	now := time.Now()
	var ids []string
	for _, id := range p.dueCards(now) {
		if _, ok := byID[id]; ok {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 && *all {
		for id := range byID {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		if len(byID) == 0 {
			fmt.Println("Nothing to review yet: take a quiz first (go run ./cmd/course quiz).")
			return nil
		}
		next := time.Time{}
		for id := range byID {
			if d := p.Cards[id].Due; next.IsZero() || d.Before(next) {
				next = d
			}
		}
		fmt.Printf("Nothing is due. The next question comes back in %s. (Use -all to review anyway.)\n",
			time.Until(next).Round(time.Hour))
		return nil
	}

	// Lowest boxes first (the shakiest knowledge), then shuffle the
	// selection so stages are interleaved.
	rand.Shuffle(len(ids), func(i, j int) { ids[i], ids[j] = ids[j], ids[i] })
	slices.SortStableFunc(ids, func(a, b string) int { return p.Cards[a].Box - p.Cards[b].Box })
	if len(ids) > *limit {
		ids = ids[:*limit]
	}
	rand.Shuffle(len(ids), func(i, j int) { ids[i], ids[j] = ids[j], ids[i] })

	fmt.Printf("%s %d question(s) from across the course.\n", c.bold("Review:"), len(ids))
	fmt.Println(c.dim("Spaced repetition: questions you get right come back later and later; misses come back soon."))
	a := &asker{in: bufio.NewScanner(os.Stdin), out: os.Stdout, c: c}
	score, asked := 0, 0
	for i, id := range ids {
		res := a.ask(byID[id], i+1, len(ids))
		if res == outQuit {
			break
		}
		asked++
		p.Cards[id].answer(res == outCorrect, now)
		if res == outCorrect {
			score++
		}
	}
	if err := p.save(); err != nil {
		return err
	}
	if asked > 0 {
		fmt.Printf("\n%s %d/%d\n", c.bold("Review score:"), score, asked)
	}
	return nil
}

// --- reset

func cmdReset(p *progress, args []string) error {
	if len(args) == 0 || args[0] != "-yes" {
		return errors.New("this forgets every passed stage and quiz result; run `course reset -yes` to confirm")
	}
	err := os.Remove(p.path)
	if errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	if err == nil {
		fmt.Println("Progress reset.")
	}
	return err
}
