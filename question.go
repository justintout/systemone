package systemone

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Content is the body of a question's instructions or of a single criterion.
// A string covers most questions. Any value that encodes to a JSON object or
// array is also accepted, which lets a question carry named data it refers to
// by backticked path. See https://docs.typesafe.ai/primitives/advanced.
type Content = any

// Question is one typed question in a request. The three implementations are
// [Choice], [Score] and [Noul]; the interface is closed.
type Question interface {
	// ID is the key the question is sent under and the answer comes back under.
	ID() string
	// encode returns the wire form, or the error collected while building it.
	encode() (any, error)
}

// QuestionError reports a question that cannot be sent as built.
type QuestionError struct {
	QuestionID string
	Err        error
}

func (e *QuestionError) Error() string {
	return fmt.Sprintf("systemone: question %q: %v", e.QuestionID, e.Err)
}

func (e *QuestionError) Unwrap() error { return e.Err }

// Errors returned when reading an answer out of a [Response].
var (
	// ErrNoAnswer means the response carries no answer under the question's ID.
	ErrNoAnswer = errors.New("no answer for question")
	// ErrAnswerType means the answer under that ID is of a different primitive.
	ErrAnswerType = errors.New("answer is of a different question type")
	// ErrIncompleteAnswer means the answer arrived without a field the API
	// documents as required, so it cannot be read without inventing a value.
	ErrIncompleteAnswer = errors.New("answer is missing a required field")
)

// question holds the fields every primitive shares.
type question struct {
	id           string
	kind         string
	instructions Content
	err          error
}

func (q *question) ID() string { return q.id }

// fail records the first build error. Later errors are dropped so the message
// points at the root cause.
func (q *question) fail(format string, args ...any) {
	if q.err == nil {
		q.err = fmt.Errorf(format, args...)
	}
}

// validate checks what every primitive requires.
func (q *question) validate() {
	if q.id == "" {
		q.fail("id is empty")
	}
	if isEmptyContent(q.instructions) {
		q.fail("instructions are empty")
	}
}

func isEmptyContent(c Content) bool {
	if c == nil {
		return true
	}
	s, ok := c.(string)
	return ok && s == ""
}

// wire is the JSON object sent for one question.
type wire struct {
	Type         string  `json:"type"`
	Instructions Content `json:"instructions"`
	Criteria     any     `json:"criteria,omitempty"`
}

// pairs marshals as a JSON object that keeps insertion order, which a plain Go
// map cannot. Choice options are sent in the order they were declared so the
// model sees them the way they were written.
type pairs []pair

type pair struct {
	key   string
	value Content
}

func (p pairs) MarshalJSON() ([]byte, error) {
	var b []byte
	b = append(b, '{')
	for i, kv := range p {
		if i > 0 {
			b = append(b, ',')
		}
		k, err := json.Marshal(kv.key)
		if err != nil {
			return nil, err
		}
		b = append(b, k...)
		b = append(b, ':')
		v, err := json.Marshal(kv.value)
		if err != nil {
			return nil, fmt.Errorf("criterion %q: %w", kv.key, err)
		}
		b = append(b, v...)
	}
	return append(b, '}'), nil
}
