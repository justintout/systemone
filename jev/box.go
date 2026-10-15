package jev

import (
	"encoding/json"
	"errors"
	"fmt"
	"image"

	"github.com/justintout/systemone"
	"github.com/justintout/systemone/internal/bridge"
)

// Box asks where something is in an image and returns the four edges of a box
// around it. It needs an image in the request and Jev 1.14 or later, so send
// it in [Request.Boxes] with the [Preview] model. It is not a
// [systemone.Question], which keeps it out of requests to deployments that do
// not answer it. See https://docs.typesafe.ai/primitives/box.
//
//	var mug = jev.NewBox("mug", "the coffee mug in `photo`")
//
// A Box answer is always a box, even when the target is absent. Ask a
// [systemone.Noul] whether the target is there in the same request, and ignore
// the box when it says no.
type Box struct {
	id           string
	instructions systemone.Content
	err          error
}

// NewBox builds a Box question. The instructions name what to find and in
// which image; they need not be a question. A Box takes no criteria. A build
// error is reported by the call that sends the question.
func NewBox(id string, instructions systemone.Content) *Box {
	q := &Box{id: id, instructions: instructions}
	switch s, isString := instructions.(string); {
	case id == "":
		q.err = errors.New("id is empty")
	case instructions == nil, isString && s == "":
		q.err = errors.New("instructions are empty")
	}
	return q
}

// ID is the key the question is sent under and the answer comes back under.
func (q *Box) ID() string { return q.id }

func (q *Box) wire() (bridge.Question, error) {
	if q.err != nil {
		return bridge.Question{}, &systemone.QuestionError{QuestionID: q.id, Err: q.err}
	}
	return bridge.Question{ID: q.id, Wire: struct {
		Type         string            `json:"type"`
		Instructions systemone.Content `json:"instructions"`
	}{"box", q.instructions}}, nil
}

// BoxAnswer is the answer to a [Box] question. Each edge is a fraction of the
// image's size measured from its top-left corner: Left and Right of the width,
// Top and Bottom of the height. Left is less than Right and Top is less than
// Bottom.
type BoxAnswer struct {
	Top, Bottom, Left, Right float64
	// Confidence is how sure the model is of the box, from 0 to 1.
	Confidence float64
}

// Rect converts the edges to pixels in an image of the given size, for
// drawing the box or cropping to it.
func (a BoxAnswer) Rect(width, height int) image.Rectangle {
	px := func(f float64, size int) int { return int(f*float64(size) + 0.5) }
	return image.Rect(px(a.Left, width), px(a.Top, height), px(a.Right, width), px(a.Bottom, height))
}

// From reads this question's answer out of a response. Its errors are the
// ones [systemone.Noul.From] returns.
func (q *Box) From(r *systemone.Response) (BoxAnswer, error) {
	fail := func(err error) (BoxAnswer, error) {
		return BoxAnswer{}, &systemone.QuestionError{QuestionID: q.id, Err: err}
	}
	data, ok := bridge.Answer(r, q.id)
	if !ok {
		return fail(systemone.ErrNoAnswer)
	}
	// Pointers tell an absent field from a real zero, so a truncated answer
	// is an error instead of a box at the image's edge.
	var raw struct {
		Type string `json:"type"`
		Box  *struct {
			Top, Bottom, Left, Right *float64
		} `json:"box"`
		Confidence *float64 `json:"confidence"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return fail(err)
	}
	if raw.Type != "box" {
		return fail(systemone.ErrAnswerType)
	}
	missing := func(field string) (BoxAnswer, error) {
		return fail(fmt.Errorf("%w: %s", systemone.ErrIncompleteAnswer, field))
	}
	switch b := raw.Box; {
	case b == nil:
		return missing("box")
	case b.Top == nil:
		return missing("box.top")
	case b.Bottom == nil:
		return missing("box.bottom")
	case b.Left == nil:
		return missing("box.left")
	case b.Right == nil:
		return missing("box.right")
	case raw.Confidence == nil:
		return missing("confidence")
	}
	return BoxAnswer{
		Top:        *raw.Box.Top,
		Bottom:     *raw.Box.Bottom,
		Left:       *raw.Box.Left,
		Right:      *raw.Box.Right,
		Confidence: *raw.Confidence,
	}, nil
}
