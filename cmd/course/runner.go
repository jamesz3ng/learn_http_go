package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// testEvent is one line of `go test -json` output (see `go doc test2json`).
type testEvent struct {
	Action  string
	Test    string
	Elapsed float64
	Output  string
}

type testResult struct {
	Name     string
	Started  bool
	Finished bool
	Passed   bool
	Elapsed  float64
	Output   []string
}

type stageResult struct {
	Tests    []*testResult
	Expected int // tests this stage has, from `go test -list`
}

// passed reports whether every one of the stage's tests ran and passed.
// Counting against the expected list matters: a test binary that crashes
// (a panic in a goroutine, "fatal error: concurrent map writes") or hangs
// until -timeout never reports the remaining tests at all.
func (r stageResult) passed() bool {
	if r.Expected == 0 || len(r.Tests) != r.Expected {
		return false
	}
	for _, t := range r.Tests {
		if !t.Passed {
			return false
		}
	}
	return true
}

var stageNumRe = regexp.MustCompile(`^TestStage(\d\d)_`)

func stagePattern(from, to int) string {
	var nums []string
	for n := from; n <= to; n++ {
		nums = append(nums, fmt.Sprintf("%02d", n))
	}
	return "^TestStage(" + strings.Join(nums, "|") + ")_"
}

// listTests asks go test which stage tests exist. It also compiles the test
// package, so a build error is reported here, before anything runs.
func listTests(root, pattern string, c colors) (map[int][]string, bool) {
	cmd := exec.Command("go", "test", "-list", pattern, "./tests/")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Println(c.red("The tests could not be built:"))
		fmt.Println(indent(strings.TrimSpace(string(out)), "  "))
		fmt.Println()
		fmt.Println(c.dim("All stage tests live in one package that imports every project (rawhttp, gobin,"))
		fmt.Println(c.dim("gocurl, kvclient, loadgen, proxy), so ANY compile error blocks every stage. Fix it first."))
		return nil, false
	}
	byStage := map[int][]string{}
	for _, line := range strings.Split(string(out), "\n") {
		if m := stageNumRe.FindStringSubmatch(line); m != nil {
			n, _ := strconv.Atoi(m[1])
			byStage[n] = append(byStage[n], strings.TrimSpace(line))
		}
	}
	return byStage, true
}

// runStageTests runs the tests for stages from..to and prints a report.
func runStageTests(root string, from, to int, race, verbose bool, c colors) (map[int]stageResult, error) {
	pattern := stagePattern(from, to)
	expected, ok := listTests(root, pattern, c)
	if !ok {
		return map[int]stageResult{}, nil
	}

	args := []string{"test", "-json", "-count=1", "-timeout=5m"}
	if race {
		args = append(args, "-race")
	}
	args = append(args, "-run", pattern, "./tests/")

	fmt.Println(c.dim("$ go " + strings.Join(args[:len(args)-2], " ") + " -run '" + pattern + "' ./tests/"))
	cmd := exec.Command("go", args...)
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	results := map[int]stageResult{}
	for n := from; n <= to; n++ {
		results[n] = stageResult{Expected: len(expected[n])}
	}
	tests := map[string]*testResult{}
	var pkgOutput []string
	printedStage := 0
	header := func(n int) {
		if n != printedStage {
			s, _ := stageByNum(n)
			fmt.Printf("\n%s %s\n", c.bold(s.label()), c.dim("· "+s.Title+" ("+s.Project+")"))
			printedStage = n
		}
	}

	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		var ev testEvent
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		if ev.Test == "" {
			if ev.Action == "output" || ev.Action == "build-output" {
				pkgOutput = append(pkgOutput, ev.Output)
			}
			continue
		}
		top, _, _ := strings.Cut(ev.Test, "/") // fold subtests into their parent
		m := stageNumRe.FindStringSubmatch(top)
		if m == nil {
			continue
		}
		n, _ := strconv.Atoi(m[1])
		tr := tests[top]
		if tr == nil {
			tr = &testResult{Name: top}
			tests[top] = tr
		}
		switch ev.Action {
		case "run":
			tr.Started = true
		case "output":
			tr.Output = append(tr.Output, ev.Output)
		case "pass", "fail":
			if ev.Test != top {
				continue // a subtest finished; wait for the parent
			}
			tr.Finished = true
			tr.Passed = ev.Action == "pass"
			tr.Elapsed = ev.Elapsed
			sr := results[n]
			sr.Tests = append(sr.Tests, tr)
			results[n] = sr
			header(n)
			printTest(tr, verbose, c)
		}
	}
	waitErr := cmd.Wait()

	// Tests that started but never finished crashed the test binary or hung;
	// tests that never started were skipped because of that. Both fail.
	crashed := false
	for n := from; n <= to; n++ {
		for _, name := range expected[n] {
			tr := tests[name]
			if tr != nil && tr.Finished {
				continue
			}
			if tr == nil {
				tr = &testResult{Name: name}
			}
			sr := results[n]
			sr.Tests = append(sr.Tests, tr)
			results[n] = sr
			header(n)
			if tr.Started {
				crashed = true
				fmt.Printf("  %s %s %s\n", c.red("✗"), c.bold(displayName(name)),
					c.red("(crashed or hung: the whole test binary stopped here)"))
				for _, line := range tr.Output {
					fmt.Println("    " + strings.TrimRight(line, "\n"))
				}
			} else {
				fmt.Printf("  %s %s %s\n", c.red("✗"), displayName(name), c.dim("(did not run)"))
			}
		}
	}
	if crashed || (waitErr != nil && strings.Contains(strings.Join(pkgOutput, ""), "panic:")) {
		fmt.Println()
		fmt.Println(c.red("The test binary crashed or timed out, so later tests didn't run."))
		fmt.Println(c.dim("Look for \"panic:\" or \"fatal error:\" above. \"concurrent map writes\" means shared state needs a mutex."))
		if out := strings.TrimSpace(strings.Join(pkgOutput, "")); out != "" {
			fmt.Println(indent(lastLines(out, 30), "  "))
		}
	} else if waitErr != nil && len(tests) == 0 {
		fmt.Println(c.red("go test failed: " + waitErr.Error()))
		if out := strings.TrimSpace(stderr.String()); out != "" {
			fmt.Println(indent(out, "  "))
		}
	}
	return results, nil
}

func displayName(test string) string {
	name := strings.TrimPrefix(stageNumRe.ReplaceAllString(test, ""), "_")
	return strings.ReplaceAll(name, "_", " ")
}

func printTest(tr *testResult, verbose bool, c colors) {
	name := displayName(tr.Name)
	if tr.Passed {
		fmt.Printf("  %s %s %s\n", c.green("✓"), name, c.dim(fmt.Sprintf("%.2fs", tr.Elapsed)))
		if !verbose {
			return
		}
	} else {
		fmt.Printf("  %s %s\n", c.red("✗"), c.bold(name))
	}
	for _, line := range tr.Output {
		t := strings.TrimRight(line, "\n")
		trimmed := strings.TrimSpace(t)
		// Skip go test's own bookkeeping lines.
		if trimmed == "" || strings.HasPrefix(trimmed, "=== ") || strings.HasPrefix(trimmed, "--- ") {
			continue
		}
		if strings.HasPrefix(trimmed, "hint:") {
			fmt.Println("      " + c.yellow(trimmed))
			continue
		}
		fmt.Println("    " + strings.TrimPrefix(t, "    ")) // go test indents by 4 already
	}
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// --- colours

type colors struct{ on bool }

func newColors() colors {
	fi, err := os.Stdout.Stat()
	tty := err == nil && fi.Mode()&os.ModeCharDevice != 0
	return colors{on: tty && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"}
}

func (c colors) wrap(code, s string) string {
	if !c.on {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func (c colors) bold(s string) string   { return c.wrap("1", s) }
func (c colors) dim(s string) string    { return c.wrap("2", s) }
func (c colors) red(s string) string    { return c.wrap("31", s) }
func (c colors) green(s string) string  { return c.wrap("32", s) }
func (c colors) yellow(s string) string { return c.wrap("33", s) }
func (c colors) cyan(s string) string   { return c.wrap("36", s) }
