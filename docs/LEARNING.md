# How this course helps you learn (and how to get the most from it)

This course is built around a few findings from learning science that hold
up well across many studies. Knowing why each part exists will help you use
it well, and skip the parts that don't suit you.

## The principles, and where they show up

**1. Learning by doing: generation beats reading.**
You remember what you *produce* far better than what you read. So every stage
makes you build something, and the reading is kept to what the task needs.
*In the course:* the stage task; "Explore further".

**2. Fast, specific feedback.**
Feedback works best when it's immediate and says what to do next.
*In the course:* the stage tests, whose failures name the problem and often
include a `hint:`. Run them early and often, even before you've written
anything, to see what's expected.

**3. Retrieval practice: testing is learning.**
Pulling an idea *out* of memory strengthens it much more than putting it in
again (re-reading). Struggling to recall is the point. It's a "desirable
difficulty".
*In the course:* `go run ./cmd/course quiz N` after each stage. Answer
from memory, before you look anything up.

**4. Spacing.**
Reviewing something just as you're about to forget it, at growing
intervals, makes it stick for months instead of days.
*In the course:* `go run ./cmd/course review` keeps a
[Leitner box](https://en.wikipedia.org/wiki/Leitner_system) for every quiz
question you've answered. A miss comes back the next session; a hit comes
back after 1, 3, 7, then 21 days. Five minutes every few days is enough.

**5. Interleaving.**
Mixing related topics, rather than doing one in a long block, helps you
tell them apart and see the connections.
*In the course:* server and client stages alternate, so each concept (bodies,
redirects, cookies, timeouts, streams) is seen from both ends of the wire.
Review sessions mix questions from across the course.

**6. Pre-testing and prediction.**
Trying to answer a question *before* you learn the answer, even if you get it
wrong, primes you to notice the answer when it appears.
*In the course:* each stage opens with "Before you read: predict", and the
experiments ask you to write down a prediction before running anything.
A wrong prediction is the most useful outcome: it shows you exactly where
your mental model is off.

**7. Worked examples, then fading support.**
Early on, studying worked examples beats solving from scratch; as you gain
skill, support should fade.
*In the course:* early stages have detailed reading and near-code hints.
Later stages give you more room and fewer steps. The test files are worked
examples of HTTP testing throughout.

**8. Elaboration and self-explanation.**
Explaining *why* something works, in your own words, links new knowledge to
what you already know.
*In the course:* "Explain it back" prompts at the end of each stage.

**9. Going to the source.**
Go's standard library is unusually readable. Reading the real code turns
"magic" into mechanism.
*In the course:* "Source dive" boxes point at specific functions. Try
`go doc -src net/http.Redirect` or open `$(go env GOROOT)/src/net/http/`.

## A suggested routine for each stage (60–120 minutes)

1. **Predict (2 min).** Answer the "Before you read" questions in your head
   or on paper. Don't look anything up.
2. **Read (15–30 min).** Type out the snippets rather than copy-pasting.
   Check your predictions as you go.
3. **Look at the tests (5 min).** Run `go run ./cmd/course test`, see red,
   and skim the stage's test file in `tests/`.
4. **Build (30–60 min).** Make the tests pass one at a time. When stuck,
   open **one** hint, then work for another 10 minutes before opening the next.
5. **Experiment (10 min).** Run the experiments. Predict first!
6. **Quiz (5 min).** `go run ./cmd/course quiz N`.
7. **Explain it back (5 min).** Write 3–5 sentences in a journal
   (a `JOURNAL.md` in this repo works well). Future-you will thank you.
8. **Explore (optional, open-ended).** Pick one challenge that interests you.

Then, every few days: `go run ./cmd/course review`.

## On hints and solutions

Struggle is productive up to a point. Being stuck for 10–15 minutes on a
well-defined problem is where the learning happens; being stuck for an hour
mostly isn't. The hints are staged so you can take the smallest nudge you
need. The reference solution exists so you're never permanently stuck, but
reading a solution teaches far less than writing one. If you do look:
close it, wait a few minutes, and re-type your version from memory.

## On going off-piste

The tests define a minimum, not a maximum. Add endpoints, flags, and
features that interest you; the "Explore further" lists are starting points.
Curiosity-driven detours are some of the best learning you'll do, as long as
the stage tests keep passing.
