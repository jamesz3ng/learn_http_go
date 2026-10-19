package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// progress is persisted to .course/progress.json in the repository root.
type progress struct {
	Passed  map[int]time.Time  `json:"passed"`
	Quizzes map[int]quizResult `json:"quizzes"`
	Cards   map[string]*card   `json:"cards"`
	path    string
}

type quizResult struct {
	Score int       `json:"score"`
	Total int       `json:"total"`
	Taken time.Time `json:"taken"`
}

// card is one quiz question in the spaced-repetition (Leitner) system.
// Box 1 is "review again today"; each correct answer moves the card up a
// box, and higher boxes wait longer before coming back.
type card struct {
	Stage   int       `json:"stage"`
	Box     int       `json:"box"`
	Due     time.Time `json:"due"`
	Seen    int       `json:"seen"`
	Correct int       `json:"correct"`
}

// boxIntervals[b] is how long a card waits after landing in box b.
var boxIntervals = map[int]time.Duration{
	1: 0,
	2: 24 * time.Hour,
	3: 3 * 24 * time.Hour,
	4: 7 * 24 * time.Hour,
	5: 21 * 24 * time.Hour,
}

func (c *card) answer(correct bool, now time.Time) {
	c.Seen++
	if correct {
		c.Correct++
		c.Box = min(c.Box+1, 5)
	} else {
		c.Box = 1
	}
	c.Due = now.Add(boxIntervals[c.Box])
}

func loadProgress(root string) (*progress, error) {
	p := &progress{
		Passed:  map[int]time.Time{},
		Quizzes: map[int]quizResult{},
		Cards:   map[string]*card{},
		path:    filepath.Join(root, ".course", "progress.json"),
	}
	b, err := os.ReadFile(p.path)
	if errors.Is(err, os.ErrNotExist) {
		return p, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, p); err != nil {
		return nil, err
	}
	if p.Passed == nil {
		p.Passed = map[int]time.Time{}
	}
	if p.Quizzes == nil {
		p.Quizzes = map[int]quizResult{}
	}
	if p.Cards == nil {
		p.Cards = map[string]*card{}
	}
	return p, nil
}

func (p *progress) save() error {
	if err := os.MkdirAll(filepath.Dir(p.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	tmp := p.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p.path)
}

// current is the first stage (from 1) that hasn't been passed.
func (p *progress) current() int {
	for n := 1; n <= lastStage; n++ {
		if _, ok := p.Passed[n]; !ok {
			return n
		}
	}
	return lastStage + 1 // finished!
}

// dueCards returns the IDs of cards due for review.
func (p *progress) dueCards(now time.Time) []string {
	var ids []string
	for id, c := range p.Cards {
		if !c.Due.After(now) {
			ids = append(ids, id)
		}
	}
	return ids
}

// pruneOrphanCards forgets review cards whose question no longer exists
// (question IDs come from the prompt text, so editing a question orphans its
// card). Without this, status would count cards that review can't show.
func (p *progress) pruneOrphanCards() {
	known := map[string]bool{}
	for n := 0; n <= lastStage; n++ {
		qs, err := loadQuiz(n)
		if err != nil {
			continue
		}
		for _, q := range qs {
			known[q.ID] = true
		}
	}
	for id := range p.Cards {
		if !known[id] {
			delete(p.Cards, id)
		}
	}
}
