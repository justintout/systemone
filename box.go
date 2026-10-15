package systemone

import "image"

// Box asks where something is in an image and returns the four edges of a box
// around it. Box questions need an image in the request and Jev 1.14 or later,
// so send them with a jev.Client and the jev.Preview model; Clef does not
// answer them. See https://docs.typesafe.ai/primitives/box.
//
//	var mug = systemone.NewBox("mug", "the coffee mug in `photo`")
//
// A Box answer is always a box, even when the target is absent. Ask a [Noul]
// whether the target is there in the same request, and ignore the box when it
// says no.
type Box struct {
	question
}

// NewBox builds a Box question. The instructions name what to find and in
// which image; they need not be a question. A Box takes no criteria.
func NewBox(id string, instructions Content) *Box {
	q := &Box{question: question{id: id, kind: "box", instructions: instructions}}
	q.validate()
	return q
}

func (q *Box) encode() (any, error) {
	if q.err != nil {
		return nil, q.err
	}
	return wire{Type: q.kind, Instructions: q.instructions}, nil
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

// From reads this question's answer out of a response. It returns a
// [*QuestionError] wrapping [ErrNoAnswer] when the response has no answer under
// the question's ID, or [ErrAnswerType] when that answer is a different
// primitive.
func (q *Box) From(r *Response) (BoxAnswer, error) {
	raw, err := r.lookup(q.id, q.kind)
	if err != nil {
		return BoxAnswer{}, err
	}
	return BoxAnswer{
		Top:        *raw.Box.Top,
		Bottom:     *raw.Box.Bottom,
		Left:       *raw.Box.Left,
		Right:      *raw.Box.Right,
		Confidence: *raw.Confidence,
	}, nil
}
