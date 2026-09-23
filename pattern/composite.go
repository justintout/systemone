package pattern

import "github.com/justintout/systemone"

// Term is one weighted dimension of a composite score. Value is expected on a
// 0 to 1 scale so dimensions with different level counts combine fairly.
type Term struct {
	Value  float64
	Weight float64
}

// Weigh turns a Score answer into a Term, normalizing it to 0 to 1.
func Weigh[T ~int](a systemone.ScoreAnswer[T], weight float64) Term {
	return Term{Value: a.Normalized(), Weight: weight}
}

// WeighNoul turns a Noul answer into a Term. The probability of yes is already
// on a 0 to 1 scale.
func WeighNoul(a systemone.NoulAnswer, weight float64) Term {
	return Term{Value: a.Value, Weight: weight}
}

// Composite is the weighted mean of the terms, or 0 when the weights sum to
// zero. Weights live in code, so changing one reranks existing answers without
// a new request.
//
// A weighted mean lets a strong dimension compensate for a weak one. When a
// single dimension must be disqualifying on its own, test it separately instead
// of folding it into the mean.
func Composite(terms ...Term) float64 {
	var sum, weights float64
	for _, t := range terms {
		sum += t.Value * t.Weight
		weights += t.Weight
	}
	if weights == 0 {
		return 0
	}
	return sum / weights
}
