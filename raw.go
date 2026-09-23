package systemone

import (
	"encoding/json"
	"errors"
)

// RawQuestion sends a question this package does not model, so a new API
// feature can be used before an SDK release adds typed support for it. Body is
// the whole question object, including its type field:
//
//	beam := systemone.RawQuestion{
//		QuestionID: "department",
//		Body: map[string]any{
//			"type":         "choice",
//			"instructions": "Which team should handle this?",
//			"criteria":     map[string]any{"billing": nil, "technical": nil},
//			"beam_width":   4,
//		},
//	}
//
// Nothing about Body is checked, and its answer comes back as undecoded JSON.
// Prefer [NewChoice], [NewNoul] and [NewScore], and upgrade the SDK when it
// grows support for what you need.
type RawQuestion struct {
	QuestionID string
	Body       Content
}

// ID returns the key this question is sent under.
func (q RawQuestion) ID() string { return q.QuestionID }

func (q RawQuestion) encode() (any, error) {
	if q.QuestionID == "" {
		return nil, errors.New("id is empty")
	}
	if q.Body == nil {
		return nil, errors.New("body is nil")
	}
	return q.Body, nil
}

// From returns this question's answer as undecoded JSON. It returns a
// [*QuestionError] wrapping [ErrNoAnswer] when the response has no answer under
// the question's ID.
func (q RawQuestion) From(r *Response) (json.RawMessage, error) {
	data, ok := r.answers[q.QuestionID]
	if !ok {
		return nil, &QuestionError{QuestionID: q.QuestionID, Err: ErrNoAnswer}
	}
	return data, nil
}
