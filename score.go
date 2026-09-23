package systemone

import (
	"encoding/json"
	"strconv"
)

// Score level count limits the API enforces.
const (
	MinScoreLevels = 2
	MaxScoreLevels = 10
)

// Score rates the state along ordered levels you define and returns a
// probability-weighted position across them. T is an integer type whose
// constants are the levels, declared from lowest to highest starting at zero:
//
//	type Frustration int
//
//	const (
//		Calm Frustration = iota
//		Frustrated
//		VeryAngry
//	)
//
//	var frustration = systemone.NewScore[Frustration]("frustration", "How frustrated is the customer?",
//		systemone.Level(Calm, "Calm"),
//		systemone.Level(Frustrated, "Frustrated"),
//		systemone.Level(VeryAngry, "Very angry"),
//	)
//
// Each level description must describe a concrete situation and stand on its
// own. See https://docs.typesafe.ai/primitives/score.
type Score[T ~int] struct {
	question
	levels []Content
}

// ScoreOption configures a [Score] during construction.
type ScoreOption[T ~int] interface {
	applyScore(*Score[T])
}

type scoreOptionFunc[T ~int] func(*Score[T])

func (f scoreOptionFunc[T]) applyScore(q *Score[T]) { f(q) }

// NewScore builds a Score question.
func NewScore[T ~int](id string, instructions Content, opts ...ScoreOption[T]) *Score[T] {
	q := &Score[T]{question: question{id: id, kind: "score", instructions: instructions}}
	for _, opt := range opts {
		opt.applyScore(q)
	}
	q.validate()
	switch {
	case len(q.levels) < MinScoreLevels:
		q.fail("%d levels, the minimum is %d", len(q.levels), MinScoreLevels)
	case len(q.levels) > MaxScoreLevels:
		q.fail("%d levels, the maximum is %d", len(q.levels), MaxScoreLevels)
	}
	return q
}

// Level declares one level and its description. Levels are sent as an ordered
// array, so level must equal the position it is declared in: the first Level
// must be 0, the second 1, and so on. A mismatch fails the request rather than
// silently shifting the scale.
func Level[T ~int](level T, description Content) ScoreOption[T] {
	return scoreOptionFunc[T](func(q *Score[T]) {
		if want := T(len(q.levels)); level != want {
			q.fail("level %d declared at position %d", int(level), int(want))
			return
		}
		if isEmptyContent(description) {
			q.fail("level %d has no description", int(level))
			return
		}
		q.levels = append(q.levels, description)
	})
}

// Levels declares every level at once, in order from lowest to highest. The
// level values are the positions in the list.
func Levels[T ~int](descriptions ...Content) ScoreOption[T] {
	return scoreOptionFunc[T](func(q *Score[T]) {
		for i, d := range descriptions {
			Level(T(i), d).applyScore(q)
		}
	})
}

// Levels returns the number of declared levels.
func (q *Score[T]) Levels() int { return len(q.levels) }

func (q *Score[T]) encode() (any, error) {
	if q.err != nil {
		return nil, q.err
	}
	return wire{Type: q.kind, Instructions: q.instructions, Criteria: q.levels}, nil
}

// ScoreAnswer is the answer to a [Score] question.
type ScoreAnswer[T ~int] struct {
	// Value is the probability-weighted position across the levels. It can land
	// between two levels, so it is a float rather than a T.
	Value float64
	// Level is the single most probable level. It can differ from rounding
	// Value when the distribution is lopsided.
	Level T
	// Probabilities maps every level to its probability. They sum to 1.
	Probabilities map[T]float64
	// Legend maps every level back to the description that was sent for it.
	// The API echoes the description as it was sent, so a level declared with
	// a structured description comes back structured. A response may carry
	// fewer entries than the question declared, so it does not give the scale.
	Legend map[T]Content
	// LevelCount is how many levels the question declared. It comes from the
	// question, not the response, and is what [ScoreAnswer.Normalized] divides
	// by. A ScoreAnswer built by hand must set it.
	LevelCount int
	// Confidence is how concentrated Probabilities is, from 0 to 1. See
	// https://docs.typesafe.ai/confidence.
	Confidence float64
}

// Probability returns the probability of one level, or 0 if the answer does not
// carry it.
func (a ScoreAnswer[T]) Probability(level T) float64 { return a.Probabilities[level] }

// Describe returns the description [ScoreAnswer.Level] was sent with, as text.
// A level declared with a structured description is rendered as JSON; reach
// into [ScoreAnswer.Legend] to read its fields.
func (a ScoreAnswer[T]) Describe() string { return describe(a.Legend[a.Level]) }

func describe(c Content) string {
	switch v := c.(type) {
	case nil:
		return ""
	case string:
		return v
	}
	text, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	return string(text)
}

// Normalized maps Value onto 0 to 1, dividing by the highest declared level.
// Use it to put scores with different level counts on one scale before
// combining them. It returns 0 when LevelCount is unset, which only happens on
// a ScoreAnswer that did not come from [Score.From].
func (a ScoreAnswer[T]) Normalized() float64 {
	top := a.LevelCount - 1
	if top < 1 {
		return 0
	}
	return a.Value / float64(top)
}

// From reads this question's answer out of a response. It returns a
// [*QuestionError] wrapping [ErrNoAnswer] when the response has no answer under
// the question's ID, or [ErrAnswerType] when that answer is a different
// primitive.
func (q *Score[T]) From(r *Response) (ScoreAnswer[T], error) {
	raw, err := r.lookup(q.id, q.kind)
	if err != nil {
		return ScoreAnswer[T]{}, err
	}
	a := ScoreAnswer[T]{
		Value:         *raw.Score,
		Confidence:    *raw.Confidence,
		LevelCount:    len(q.levels),
		Probabilities: make(map[T]float64, len(raw.Probabilities)),
		Legend:        make(map[T]Content, len(raw.Legend)),
	}
	for k, v := range raw.Probabilities {
		level, err := parseLevel[T](q.id, k)
		if err != nil {
			return ScoreAnswer[T]{}, err
		}
		a.Probabilities[level] = v
	}
	for k, v := range raw.Legend {
		level, err := parseLevel[T](q.id, k)
		if err != nil {
			return ScoreAnswer[T]{}, err
		}
		a.Legend[level] = v
	}
	a.Level = modalLevel(a.Probabilities)
	return a, nil
}

func parseLevel[T ~int](id, key string) (T, error) {
	n, err := strconv.Atoi(key)
	if err != nil {
		return 0, &QuestionError{QuestionID: id, Err: err}
	}
	return T(n), nil
}

// modalLevel returns the most probable level, breaking ties toward the lower
// level so the result does not depend on map iteration order.
func modalLevel[T ~int](probabilities map[T]float64) T {
	var best T
	first := true
	for level, p := range probabilities {
		switch {
		case first, p > probabilities[best], p == probabilities[best] && level < best:
			best, first = level, false
		}
	}
	return best
}
