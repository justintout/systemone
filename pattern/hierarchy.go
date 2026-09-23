package pattern

import (
	"context"
	"fmt"
	"slices"

	"github.com/justintout/systemone"
)

// Tree is a taxonomy. Each key is a category name and its value holds that
// category's children; a nil or empty value is a leaf.
type Tree map[string]Tree

// Step is one level of a classification walk.
type Step struct {
	// Name is the category chosen at this level.
	Name string
	// Confidence is the Choice answer's confidence at this level.
	Confidence float64
	// Probabilities are every sibling's probability at this level. Use them to
	// keep more than one branch alive when the top two are close.
	Probabilities map[string]float64
}

// Walk classifies state down a taxonomy, asking one Choice per level with the
// current node's children as the options and each child's subtree as its
// description. Showing the subtree lets the model see what lives under a branch
// before committing to it.
//
// The walk is greedy: it follows the highest-probability child at each level.
// Every step carries the full sibling distribution, so a caller that wants a
// beam search can run Walk's per-level question itself and keep several paths.
//
// It stops at a leaf, at MaxDepth, or at the first step below MinConfidence,
// returning the steps taken so far.
//
// See https://docs.typesafe.ai/cookbooks/hierarchical_classification.
func Walk(ctx context.Context, c *systemone.Client, state any, tree Tree, opts ...WalkOption) ([]Step, error) {
	w := walk{
		instructions: func(path []string) systemone.Content {
			if len(path) == 0 {
				return "Which top-level category does this belong to?"
			}
			return fmt.Sprintf("This belongs under %q. Which of its subcategories does it belong to?", path[len(path)-1])
		},
		questionID:   "category",
		subtreeDepth: 2,
	}
	for _, opt := range opts {
		opt(&w)
	}

	var steps []Step
	var path []string
	node := tree
	for len(node) > 0 {
		if w.maxDepth > 0 && len(steps) >= w.maxDepth {
			break
		}
		step, err := w.level(ctx, c, state, node, path)
		if err != nil {
			return steps, err
		}
		if step.Confidence < w.minConfidence {
			break
		}
		steps = append(steps, step)
		path = append(path, step.Name)
		node = node[step.Name]
	}
	return steps, nil
}

// WalkOption configures [Walk].
type WalkOption func(*walk)

type walk struct {
	instructions  func(path []string) systemone.Content
	questionID    string
	minConfidence float64
	maxDepth      int
	subtreeDepth  int
}

// WithInstructions replaces the per-level question text. The argument is the
// path chosen so far, empty at the top level.
func WithInstructions(f func(path []string) systemone.Content) WalkOption {
	return func(w *walk) { w.instructions = f }
}

// WithQuestionID sets the question ID used at each level. It never reaches the
// model.
func WithQuestionID(id string) WalkOption {
	return func(w *walk) { w.questionID = id }
}

// WithMinConfidence stops the walk before the first step whose confidence falls
// below min, leaving the classification at the last level it was sure of.
func WithMinConfidence(min float64) WalkOption {
	return func(w *walk) { w.minConfidence = min }
}

// WithMaxDepth stops the walk after n levels. Zero, the default, walks to a
// leaf.
func WithMaxDepth(n int) WalkOption {
	return func(w *walk) { w.maxDepth = n }
}

// WithSubtreeDepth limits how many levels of each option's subtree are shown to
// the model. Deep subtrees cost tokens and crowd the question; the default
// shows two levels. Zero shows the option names alone.
func WithSubtreeDepth(n int) WalkOption {
	return func(w *walk) { w.subtreeDepth = n }
}

func (w walk) level(ctx context.Context, c *systemone.Client, state any, node Tree, path []string) (Step, error) {
	names := make([]string, 0, len(node))
	for name := range node {
		names = append(names, name)
	}
	slices.Sort(names)

	opts := make([]systemone.ChoiceOption[string], 0, len(names))
	for _, name := range names {
		opts = append(opts, systemone.Option(name, trim(node[name], w.subtreeDepth)))
	}
	q := systemone.NewChoice(w.questionID, w.instructions(path), opts...)

	res, err := c.Ask(ctx, state, q)
	if err != nil {
		return Step{}, err
	}
	a, err := q.From(res)
	if err != nil {
		return Step{}, err
	}
	return Step{Name: a.Value, Confidence: a.Confidence, Probabilities: a.Probabilities}, nil
}

// trim returns the subtree cut to depth levels, or nil when nothing is left to
// show. A trimmed branch is sent as an option with no description.
func trim(t Tree, depth int) systemone.Content {
	if len(t) == 0 || depth <= 0 {
		return nil
	}
	out := make(Tree, len(t))
	for name, child := range t {
		sub := trim(child, depth-1)
		if sub == nil {
			out[name] = nil
			continue
		}
		out[name] = sub.(Tree)
	}
	return out
}
