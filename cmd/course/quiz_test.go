package main

import "testing"

// TestQuizzesParse checks every stage has a well-formed quiz.
func TestQuizzesParse(t *testing.T) {
	ids := map[string]bool{}
	for n := 0; n <= lastStage; n++ {
		qs, err := loadQuiz(n)
		if err != nil {
			t.Errorf("stage %d: %v", n, err)
			continue
		}
		if len(qs) < 5 {
			t.Errorf("stage %d: only %d questions", n, len(qs))
		}
		for _, q := range qs {
			if q.Explanation == "" {
				t.Errorf("stage %d: question %q has no explanation", n, firstLine(q.Prompt))
			}
			if ids[q.ID] {
				t.Errorf("stage %d: duplicate question %q", n, firstLine(q.Prompt))
			}
			ids[q.ID] = true
			if len(q.Options) > 10 {
				t.Errorf("stage %d: question %q has too many options", n, firstLine(q.Prompt))
			}
		}
	}
}

func TestParseQuiz(t *testing.T) {
	src := "# comment\n\n" +
		"## What does this print?\n```go\n- not an option\n## not a question\n```\n- wrong\n* right\n> because\n> reasons\n\n" +
		"## Name the method.\n= ServeHTTP\n= ServeHTTP(w, r)\n> It's ServeHTTP.\n"
	qs, err := parseQuiz(3, src)
	if err != nil {
		t.Fatal(err)
	}
	if len(qs) != 2 {
		t.Fatalf("got %d questions", len(qs))
	}
	q := qs[0]
	want := "What does this print?\n```go\n- not an option\n## not a question\n```"
	if q.Prompt != want || len(q.Options) != 2 || !q.Options[1].Correct || q.Explanation != "because\nreasons" {
		t.Fatalf("question 0 parsed as %+v", q)
	}
	if qs[1].Answers[0] != "ServeHTTP" || normalize("  `servehttp()` ") != normalize(qs[1].Answers[0]) {
		t.Fatalf("question 1 parsed as %+v", qs[1])
	}
	if _, err := parseQuiz(1, "## no answers\n> hmm\n"); err == nil {
		t.Fatal("expected an error for a question without answers")
	}
}
