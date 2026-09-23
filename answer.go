package systemone

import (
	"encoding/json"
	"fmt"
	"slices"
)

// Usage is the token count the request was billed on. Only input tokens are
// charged; see https://docs.typesafe.ai/models.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Response is the result of one evaluation. Answers are read through the
// question handles that produced them: [Choice.From], [Score.From],
// [Noul.From].
type Response struct {
	// Model is the versioned model that answered, even when the request named
	// an alias such as jev-latest.
	Model string
	// Usage is the token count for the request.
	Usage Usage
	// RequestID is the x-typesafe-request-id response header, empty if absent.
	RequestID string

	answers map[string]json.RawMessage
	raw     []byte
}

// IDs returns the question IDs the response carries, sorted. It includes IDs
// whose answers this package does not recognize.
func (r *Response) IDs() []string {
	ids := make([]string, 0, len(r.answers))
	for id := range r.answers {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// Raw returns the undecoded response body, for logging and for inspecting an
// answer this package does not recognize. The caller must not modify the
// returned bytes.
func (r *Response) Raw() []byte { return r.raw }

// rawAnswer is the union of the three answer shapes, decoded when a handle
// narrows it to a typed answer.
// The value-bearing fields are pointers so an absent field is distinguishable
// from a real zero. Without that, a truncated noul answer reads as a confident
// "no" and a truncated score answer reads as the lowest level, which is worse
// than an error: the whole point of reaching an answer through a checked handle
// is that it cannot quietly hand back something made up.
type rawAnswer struct {
	Type          string             `json:"type"`
	Noul          *float64           `json:"noul"`
	Choice        *string            `json:"choice"`
	Score         *float64           `json:"score"`
	Legend        map[string]Content `json:"legend"`
	Probabilities map[string]float64 `json:"probabilities"`
	Confidence    *float64           `json:"confidence"`
}

// validate checks the fields the API documents as required for this primitive.
func (a rawAnswer) validate(kind string) error {
	missing := func(field string) error {
		return fmt.Errorf("%w: %s", ErrIncompleteAnswer, field)
	}
	switch kind {
	case "noul":
		if a.Noul == nil {
			return missing("noul")
		}
	case "choice":
		if a.Choice == nil {
			return missing("choice")
		}
		if a.Confidence == nil {
			return missing("confidence")
		}
		if len(a.Probabilities) == 0 {
			return missing("probabilities")
		}
	case "score":
		if a.Score == nil {
			return missing("score")
		}
		if a.Confidence == nil {
			return missing("confidence")
		}
		if len(a.Probabilities) == 0 {
			return missing("probabilities")
		}
		if len(a.Legend) == 0 {
			return missing("legend")
		}
	}
	return nil
}

type responseBody struct {
	Model   string                     `json:"model"`
	Answers map[string]json.RawMessage `json:"answers"`
	Usage   Usage                      `json:"usage"`
}

// lookup finds the answer for id and checks it is of the expected primitive.
func (r *Response) lookup(id, kind string) (rawAnswer, error) {
	data, ok := r.answers[id]
	if !ok {
		return rawAnswer{}, &QuestionError{QuestionID: id, Err: ErrNoAnswer}
	}
	var a rawAnswer
	if err := json.Unmarshal(data, &a); err != nil {
		return rawAnswer{}, &QuestionError{QuestionID: id, Err: err}
	}
	if a.Type != kind {
		return rawAnswer{}, &QuestionError{QuestionID: id, Err: ErrAnswerType}
	}
	if err := a.validate(kind); err != nil {
		return rawAnswer{}, &QuestionError{QuestionID: id, Err: err}
	}
	return a, nil
}

func decodeResponse(data []byte) (*Response, error) {
	var body responseBody
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, err
	}
	return &Response{
		Model:   body.Model,
		Usage:   body.Usage,
		answers: body.Answers,
		raw:     data,
	}, nil
}
