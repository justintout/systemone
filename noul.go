package systemone

// Noul asks a yes/no question and returns the probability that the answer is
// yes. A value near 0.5 means yes and no are close to equally likely, not that
// the condition half holds; see https://docs.typesafe.ai/primitives/noul.
//
//	var urgent = systemone.NewNoul("is_urgent", "Does this convey urgency?",
//		systemone.Yes("Explicitly time-sensitive"),
//		systemone.No("No urgency expressed"),
//	)
//
// Use one Noul per label when several labels may apply at once.
type Noul struct {
	question
	criteria pairs
}

// NoulOption configures a [Noul] during construction.
type NoulOption interface {
	applyNoul(*Noul)
}

type noulOptionFunc func(*Noul)

func (f noulOptionFunc) applyNoul(q *Noul) { f(q) }

// NewNoul builds a Noul question. Criteria are optional; describe both sides
// when the boundary between yes and no is subtle.
func NewNoul(id string, instructions Content, opts ...NoulOption) *Noul {
	q := &Noul{question: question{id: id, kind: "noul", instructions: instructions}}
	for _, opt := range opts {
		opt.applyNoul(q)
	}
	q.validate()
	return q
}

// Yes describes what a yes, a value near 1, means.
func Yes(description Content) NoulOption {
	return noulOptionFunc(func(q *Noul) { q.describe("true", description) })
}

// No describes what a no, a value near 0, means.
func No(description Content) NoulOption {
	return noulOptionFunc(func(q *Noul) { q.describe("false", description) })
}

func (q *Noul) describe(key string, description Content) {
	for _, p := range q.criteria {
		if p.key == key {
			q.fail("criterion %q declared twice", key)
			return
		}
	}
	q.criteria = append(q.criteria, pair{key: key, value: description})
}

func (q *Noul) encode() (any, error) {
	if q.err != nil {
		return nil, q.err
	}
	w := wire{Type: q.kind, Instructions: q.instructions}
	if len(q.criteria) > 0 {
		w.Criteria = q.criteria
	}
	return w, nil
}

// NoulAnswer is the answer to a [Noul] question. Noul answers carry no
// confidence: the probability is the whole answer.
type NoulAnswer struct {
	// Value is the probability that the answer is yes, from 0 to 1.
	Value float64
}

// Yes reports whether the probability of yes is at least threshold. Pick the
// threshold from your own data and the cost of being wrong.
func (a NoulAnswer) Yes(threshold float64) bool { return a.Value >= threshold }

// From reads this question's answer out of a response. It returns a
// [*QuestionError] wrapping [ErrNoAnswer] when the response has no answer under
// the question's ID, or [ErrAnswerType] when that answer is a different
// primitive.
func (q *Noul) From(r *Response) (NoulAnswer, error) {
	raw, err := r.lookup(q.id, q.kind)
	if err != nil {
		return NoulAnswer{}, err
	}
	return NoulAnswer{Value: *raw.Noul}, nil
}
