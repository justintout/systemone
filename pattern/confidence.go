package pattern

// Band is a coarse reading of an answer's confidence, used to pick how much
// autonomy to give a decision. See https://docs.typesafe.ai/confidence.
type Band int

const (
	// Low means do not act: route to a person, ask for clarification, or fall
	// back to another system.
	Low Band = iota
	// Medium means proceed with care: confirm, flag for review, or gather more
	// evidence first.
	Medium
	// High means act automatically.
	High
)

func (b Band) String() string {
	switch b {
	case Low:
		return "low"
	case Medium:
		return "medium"
	case High:
		return "high"
	}
	return "unknown"
}

// Thresholds splits confidence into three bands. Both boundaries are inclusive
// lower bounds, so confidence exactly equal to High reads as High.
//
// Set them per action rather than per system: showing the wrong screen and
// approving a transfer deserve different bars.
type Thresholds struct {
	Medium float64
	High   float64
}

// Band places a confidence value.
func (t Thresholds) Band(confidence float64) Band {
	switch {
	case confidence >= t.High:
		return High
	case confidence >= t.Medium:
		return Medium
	}
	return Low
}
