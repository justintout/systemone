package systemone

import (
	"cmp"
	"slices"
)

// MaxChoiceOptions is the largest number of options the API accepts on one
// Choice question.
const MaxChoiceOptions = 255

// Choice asks the model to pick one option from a set you define. T is the Go
// type of the options, so both the declared set and the answer are checked at
// compile time:
//
//	type Dept string
//
//	const (
//		Billing   Dept = "billing"
//		Technical Dept = "technical"
//	)
//
//	var dept = systemone.NewChoice[Dept]("department", "Which team should handle this?",
//		systemone.Option(Billing, "Payments, invoicing, refunds"),
//		systemone.Option(Technical, "Bugs, outages, integrations"),
//	)
//
// See https://docs.typesafe.ai/primitives/choice.
type Choice[T ~string] struct {
	question
	options pairs
}

// ChoiceOption configures a [Choice] during construction.
type ChoiceOption[T ~string] interface {
	applyChoice(*Choice[T])
}

type choiceOptionFunc[T ~string] func(*Choice[T])

func (f choiceOptionFunc[T]) applyChoice(c *Choice[T]) { f(c) }

// NewChoice builds a Choice question. A build error, such as a duplicate
// option, is reported by the [Client] call that sends the question rather than
// here, so questions can be declared as package-level variables.
func NewChoice[T ~string](id string, instructions Content, opts ...ChoiceOption[T]) *Choice[T] {
	c := &Choice[T]{question: question{id: id, kind: "choice", instructions: instructions}}
	for _, opt := range opts {
		opt.applyChoice(c)
	}
	c.validate()
	switch {
	case len(c.options) == 0:
		c.fail("no options")
	case len(c.options) > MaxChoiceOptions:
		c.fail("%d options, the maximum is %d", len(c.options), MaxChoiceOptions)
	}
	return c
}

// Option declares one selectable option and the rubric that describes it. A nil
// description sends the option with no extra detail. The description may be a
// string or any JSON-encodable structure; see
// https://docs.typesafe.ai/primitives/advanced.
func Option[T ~string](value T, description Content) ChoiceOption[T] {
	return choiceOptionFunc[T](func(c *Choice[T]) { c.add(value, description) })
}

// Options declares several options at once, none of them described. Use it when
// the option names carry the whole meaning.
func Options[T ~string](values ...T) ChoiceOption[T] {
	return choiceOptionFunc[T](func(c *Choice[T]) {
		for _, v := range values {
			c.add(v, nil)
		}
	})
}

func (q *Choice[T]) add(value T, description Content) {
	key := string(value)
	if key == "" {
		q.fail("option with an empty name")
		return
	}
	if slices.ContainsFunc(q.options, func(p pair) bool { return p.key == key }) {
		q.fail("duplicate option %q", key)
		return
	}
	q.options = append(q.options, pair{key: key, value: description})
}

// Options returns the declared options in declaration order.
func (q *Choice[T]) Options() []T {
	out := make([]T, len(q.options))
	for i, p := range q.options {
		out[i] = T(p.key)
	}
	return out
}

func (q *Choice[T]) encode() (any, error) {
	if q.err != nil {
		return nil, q.err
	}
	return wire{Type: q.kind, Instructions: q.instructions, Criteria: q.options}, nil
}

// ChoiceAnswer is the answer to a [Choice] question.
type ChoiceAnswer[T ~string] struct {
	// Value is the highest-probability option.
	Value T
	// Probabilities maps every declared option to its probability. They sum to 1.
	Probabilities map[T]float64
	// Confidence is how concentrated Probabilities is, from 0 to 1. It says
	// whether to act on Value, not whether Value is correct. See
	// https://docs.typesafe.ai/confidence.
	Confidence float64
}

// Probability returns the probability of one option, or 0 if the answer does
// not carry it.
func (a ChoiceAnswer[T]) Probability(option T) float64 {
	return a.Probabilities[option]
}

// Ranked returns the options ordered by descending probability. Equal
// probabilities are ordered by option name so the result is stable.
func (a ChoiceAnswer[T]) Ranked() []T {
	out := make([]T, 0, len(a.Probabilities))
	for opt := range a.Probabilities {
		out = append(out, opt)
	}
	slices.SortFunc(out, func(x, y T) int {
		if c := cmp.Compare(a.Probabilities[y], a.Probabilities[x]); c != 0 {
			return c
		}
		return cmp.Compare(x, y)
	})
	return out
}

// From reads this question's answer out of a response. It returns a
// [*QuestionError] wrapping [ErrNoAnswer] when the response has no answer under
// the question's ID, or [ErrAnswerType] when that answer is a different
// primitive.
func (q *Choice[T]) From(r *Response) (ChoiceAnswer[T], error) {
	raw, err := r.lookup(q.id, q.kind)
	if err != nil {
		return ChoiceAnswer[T]{}, err
	}
	a := ChoiceAnswer[T]{
		Value:         T(*raw.Choice),
		Confidence:    *raw.Confidence,
		Probabilities: make(map[T]float64, len(raw.Probabilities)),
	}
	for k, v := range raw.Probabilities {
		a.Probabilities[T(k)] = v
	}
	return a, nil
}
