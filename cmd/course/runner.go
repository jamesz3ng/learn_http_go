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
	Name    string
	Passed  bool
	Elapsed float64
	Output  []string
}

type stageResult struct {
	Tests []*testResult
	Ran   bool
}

func (r stageResult) passed() bool {
	if !r.Ran || len(r.Tests) == 0 {
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

// runStageTests runs the tests for stages from..to and prints a report.
func runStageTests(root string, from, to int, race, verbose bool, c colors) (map[int]stageResult, error) {
	var nums []string
	for n := from; n <= to; n++ {
		nums = append(nums, fmt.Sprintf("%02d", n))
	}
	pattern := "^TestStage(" + strings.Join(nums, "|") + ")_"
	args := []string{"test", "-json", "-count=1", "-timeout=5m"}
	if race {
		args = append(args, "-race")
	}
	args = append(args, "-run", pattern, "./tests/")

	fmt.Println(c.dim("$ go " + strings.Join(args, " ")))
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
	tests := map[string]*testResult{}
	var pkgOutput []string
	printedStage := 0

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
		case "output":
			tr.Output = append(tr.Output, ev.Output)
		case "pass", "fail":
			if ev.Test != top {
				continue // a subtest finished; wait for the parent
			}
			tr.Passed = ev.Action == "pass"
			tr.Elapsed = ev.Elapsed
			sr := results[n]
			sr.Ran = true
			sr.Tests = append(sr.Tests, tr)
			results[n] = sr

			if n != printedStage {
				s, _ := stageByNum(n)
				fmt.Printf("\n%s %s\n", c.bold(s.label()), c.dim("· "+s.Title+" ("+s.Project+")"))
				printedStage = n
			}
			printTest(tr, verbose, c)
		}
	}
	waitErr := cmd.Wait()

	// Nothing ran: most likely the code doesn't compile. Show why.
	if len(results) == 0 {
		fmt.Println()
		out := strings.TrimSpace(stderr.String() + strings.Join(pkgOutput, ""))
		if out != "" {
			fmt.Println(c.red("The tests could not run:"))
			fmt.Println(indent(out, "  "))
		} else if waitErr != nil {
			fmt.Println(c.red("go test failed: " + waitErr.Error()))
		}
		return results, nil
	}
	// A test that panics kills the whole test binary; say so.
	if waitErr != nil && strings.Contains(strings.Join(pkgOutput, ""), "panic:") {
		fmt.Println()
		fmt.Println(c.red("A test panicked, which stopped the remaining tests. Output:"))
		fmt.Println(indent(lastLines(strings.Join(pkgOutput, ""), 30), "  "))
	}
	return results, nil
}

func printTest(tr *testResult, verbose bool, c colors) {
	name := strings.TrimPrefix(stageNumRe.ReplaceAllString(tr.Name, ""), "_")
	name = strings.ReplaceAll(name, "_", " ")
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
