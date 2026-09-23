package pattern_test

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/justintout/systemone"
	"github.com/justintout/systemone/pattern"
)

func TestBand(t *testing.T) {
	th := pattern.Thresholds{Medium: 0.5, High: 0.9}
	tests := []struct {
		confidence float64
		want       pattern.Band
		wantName   string
	}{
		{0.0, pattern.Low, "low"},
		{0.49, pattern.Low, "low"},
		{0.5, pattern.Medium, "medium"},
		{0.89, pattern.Medium, "medium"},
		{0.9, pattern.High, "high"},
		{1.0, pattern.High, "high"},
	}
	for _, tt := range tests {
		got := th.Band(tt.confidence)
		if got != tt.want {
			t.Errorf("Band(%v) = %v, want %v", tt.confidence, got, tt.want)
		}
		if got.String() != tt.wantName {
			t.Errorf("Band(%v).String() = %q, want %q", tt.confidence, got, tt.wantName)
		}
	}
	if got := pattern.Band(9).String(); got != "unknown" {
		t.Errorf("Band(9).String() = %q", got)
	}
}

type Anger int

func TestComposite(t *testing.T) {
	// A three-level Score at level 2 normalizes to 1.0, and at level 1 to 0.5,
	// so scores with different level counts combine on one scale.
	anger := systemone.ScoreAnswer[Anger]{
		Value:      1,
		LevelCount: 3,
		Legend:     map[Anger]systemone.Content{0: "calm", 1: "annoyed", 2: "furious"},
	}
	urgent := systemone.NoulAnswer{Value: 0.25}

	tests := []struct {
		name  string
		terms []pattern.Term
		want  float64
	}{
		{"no terms", nil, 0},
		{"zero weights", []pattern.Term{{Value: 1, Weight: 0}}, 0},
		{"weighted mean", []pattern.Term{
			{Value: 1, Weight: 0.4}, {Value: 0.5, Weight: 0.4}, {Value: 0, Weight: 0.2},
		}, 0.6},
		{"weights need not sum to one", []pattern.Term{
			{Value: 1, Weight: 3}, {Value: 0, Weight: 1},
		}, 0.75},
		{"from answers", []pattern.Term{
			pattern.Weigh(anger, 0.5), pattern.WeighNoul(urgent, 0.5),
		}, 0.375},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pattern.Composite(tt.terms...); math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("Composite = %v, want %v", got, tt.want)
			}
		})
	}
}

// walkStub answers every Choice with the alphabetically first option, so a walk
// descends the tree deterministically, and records what it was asked.
func walkStub(t *testing.T, confidence float64) (*systemone.Client, *[]map[string]any) {
	t.Helper()
	var sent []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		var body map[string]any
		if err := json.Unmarshal(data, &body); err != nil {
			t.Errorf("request body: %v", err)
			return
		}
		sent = append(sent, body)

		questions := body["questions"].(map[string]any)
		var id string
		var criteria map[string]any
		for k, v := range questions {
			id, criteria = k, v.(map[string]any)["criteria"].(map[string]any)
		}
		pick := ""
		for option := range criteria {
			if pick == "" || option < pick {
				pick = option
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "jev-1.13.0",
			"answers": map[string]any{id: map[string]any{
				"type": "choice", "choice": pick,
				"probabilities": map[string]float64{pick: 1},
				"confidence":    confidence,
			}},
			"usage": map[string]int{"input_tokens": 1, "output_tokens": 1},
		})
	}))
	t.Cleanup(srv.Close)

	c, err := systemone.New(systemone.WithAPIKey("k"), systemone.WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	return c, &sent
}

func TestWalk(t *testing.T) {
	// "a" sorts first at every level, so the walk lands on a > a1 > a1x.
	tree := pattern.Tree{
		"a": {"a1": {"a1x": nil, "a1y": nil}, "a2": nil},
		"b": {"b1": nil},
	}

	tests := []struct {
		name       string
		confidence float64
		opts       []pattern.WalkOption
		wantPath   []string
		check      func(t *testing.T, sent []map[string]any)
	}{{
		name:       "walks to a leaf",
		confidence: 0.9,
		wantPath:   []string{"a", "a1", "a1x"},
		check: func(t *testing.T, sent []map[string]any) {
			first := sent[0]["questions"].(map[string]any)["category"].(map[string]any)
			criteria := first["criteria"].(map[string]any)
			sub, ok := criteria["a"].(map[string]any)
			if !ok {
				t.Fatalf("option a = %v, want its subtree", criteria["a"])
			}
			if _, ok := sub["a1"]; !ok {
				t.Errorf("subtree = %v, want a1", sub)
			}
		},
	}, {
		name:       "max depth stops the walk early",
		confidence: 0.9,
		opts:       []pattern.WalkOption{pattern.WithMaxDepth(2)},
		wantPath:   []string{"a", "a1"},
	}, {
		name:       "low confidence stops before the first step",
		confidence: 0.1,
		opts:       []pattern.WalkOption{pattern.WithMinConfidence(0.5)},
		wantPath:   nil,
	}, {
		name:       "question id is configurable",
		confidence: 0.9,
		opts:       []pattern.WalkOption{pattern.WithQuestionID("dept"), pattern.WithMaxDepth(1)},
		wantPath:   []string{"a"},
		check: func(t *testing.T, sent []map[string]any) {
			if _, ok := sent[0]["questions"].(map[string]any)["dept"]; !ok {
				t.Errorf("questions = %v, want dept", sent[0]["questions"])
			}
		},
	}, {
		name:       "instructions are configurable and see the path so far",
		confidence: 0.9,
		opts: []pattern.WalkOption{
			pattern.WithInstructions(func(path []string) systemone.Content {
				if len(path) == 0 {
					return "top"
				}
				return "under " + path[len(path)-1]
			}),
			pattern.WithMaxDepth(2),
		},
		wantPath: []string{"a", "a1"},
		check: func(t *testing.T, sent []map[string]any) {
			want := []string{"top", "under a"}
			for i, w := range want {
				q := sent[i]["questions"].(map[string]any)["category"].(map[string]any)
				if q["instructions"] != w {
					t.Errorf("instructions[%d] = %v, want %q", i, q["instructions"], w)
				}
			}
		},
	}, {
		name:       "subtree depth zero sends option names alone",
		confidence: 0.9,
		opts:       []pattern.WalkOption{pattern.WithSubtreeDepth(0), pattern.WithMaxDepth(1)},
		wantPath:   []string{"a"},
		check: func(t *testing.T, sent []map[string]any) {
			criteria := sent[0]["questions"].(map[string]any)["category"].(map[string]any)["criteria"].(map[string]any)
			for option, value := range criteria {
				if value != nil {
					t.Errorf("option %q = %v, want null", option, value)
				}
			}
		},
	}, {
		name:       "subtree depth one trims grandchildren",
		confidence: 0.9,
		opts:       []pattern.WalkOption{pattern.WithSubtreeDepth(1), pattern.WithMaxDepth(1)},
		wantPath:   []string{"a"},
		check: func(t *testing.T, sent []map[string]any) {
			criteria := sent[0]["questions"].(map[string]any)["category"].(map[string]any)["criteria"].(map[string]any)
			sub := criteria["a"].(map[string]any)
			if sub["a1"] != nil {
				t.Errorf("a1 = %v, want null at depth 1", sub["a1"])
			}
		},
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, sent := walkStub(t, tt.confidence)
			steps, err := pattern.Walk(context.Background(), c, "item", tree, tt.opts...)
			if err != nil {
				t.Fatal(err)
			}
			if len(steps) != len(tt.wantPath) {
				t.Fatalf("steps = %+v, want %v", steps, tt.wantPath)
			}
			for i, want := range tt.wantPath {
				if steps[i].Name != want {
					t.Errorf("step %d = %q, want %q", i, steps[i].Name, want)
				}
				if steps[i].Probabilities[want] != 1 {
					t.Errorf("step %d carries no distribution: %+v", i, steps[i])
				}
			}
			if tt.check != nil {
				tt.check(t, *sent)
			}
		})
	}
}

func TestWalkSurfacesRequestErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	c, err := systemone.New(systemone.WithAPIKey("k"), systemone.WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pattern.Walk(context.Background(), c, "item", pattern.Tree{"a": nil}); err == nil {
		t.Error("want an error")
	}
}
