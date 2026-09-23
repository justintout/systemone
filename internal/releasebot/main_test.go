package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justintout/systemone"
)

func TestCompose(t *testing.T) {
	const (
		added      = "Compatible changes:\n- (*Choice[T]).Describe: added"
		removed    = "Incompatible changes:\n- NoulAnswer.Yes: changed from func(float64) bool"
		unchanged  = ""
		definitely = 0.95
		unlikely   = 0.05
	)

	tests := []struct {
		name        string
		api         string
		worth       float64
		contract    float64
		wantRelease bool
		wantBump    string
	}{{
		name: "an added API is a minor whatever the model says",
		api:  added, worth: unlikely, contract: unlikely,
		wantRelease: true, wantBump: "minor",
	}, {
		name: "a removed API is a minor, not a major, because this module stays on v0",
		api:  removed, worth: definitely, contract: definitely,
		wantRelease: true, wantBump: "minor",
	}, {
		name: "nothing reaching consumers is not released",
		api:  unchanged, worth: unlikely, contract: definitely,
		wantRelease: false,
	}, {
		name: "a behavior change behind an unchanged API is a minor",
		api:  unchanged, worth: definitely, contract: definitely,
		wantRelease: true, wantBump: "minor",
	}, {
		name: "a fix that breaks nobody is a patch",
		api:  unchanged, worth: definitely, contract: unlikely,
		wantRelease: true, wantBump: "patch",
	}, {
		// Between the thresholds the bias runs toward minor: shipping a
		// breaking change as a patch is the costlier mistake.
		name: "an uncertain behavior call is promoted to minor",
		api:  unchanged, worth: definitely, contract: 0.4,
		wantRelease: true, wantBump: "minor",
	}, {
		// Both thresholds compare with >=, so the boundary itself decides.
		name: "exactly at the release-worthy threshold still releases",
		api:  unchanged, worth: releaseWorthyAt, contract: unlikely,
		wantRelease: true, wantBump: "patch",
	}, {
		name: "just under the release-worthy threshold does not",
		api:  unchanged, worth: releaseWorthyAt - 0.01, contract: definitely,
		wantRelease: false,
	}, {
		name: "exactly at the contract threshold is a minor",
		api:  unchanged, worth: definitely, contract: contractChangeAt,
		wantRelease: true, wantBump: "minor",
	}, {
		name: "just under the contract threshold is a patch",
		api:  unchanged, worth: definitely, contract: contractChangeAt - 0.01,
		wantRelease: true, wantBump: "patch",
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compose(tt.api,
				systemone.NoulAnswer{Value: tt.worth},
				systemone.NoulAnswer{Value: tt.contract})
			if got.Release != tt.wantRelease {
				t.Errorf("Release = %v, want %v", got.Release, tt.wantRelease)
			}
			if got.Bump != tt.wantBump {
				t.Errorf("Bump = %q, want %q", got.Bump, tt.wantBump)
			}
			if got.Reason == "" {
				t.Error("a decision with no reason is not reviewable")
			}
		})
	}
}

// emit writes the multiline GITHUB_OUTPUT format, whose contract lives in the
// Actions runner rather than in Go, so nothing else can catch a mistake in it.
func TestEmit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "output")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_OUTPUT", path)

	if err := emit(decision{Release: true, Bump: "minor", Reason: "line one\nline two"}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "release=true\nbump=minor\nreason<<REASON_EOF\nline one\nline two\nREASON_EOF\n"
	if string(got) != want {
		t.Errorf("GITHUB_OUTPUT =\n%q\nwant\n%q", got, want)
	}
}

func TestSubjects(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int
	}{
		{"empty", "", 0},
		{"only whitespace", "\n  \n", 0},
		{"one subject", "Add a thing", 1},
		{"several", "Add a thing\nFix another", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := subjects(tt.in); len(got) != tt.want {
				t.Errorf("subjects(%q) = %#v, want %d entries", tt.in, got, tt.want)
			}
		})
	}
}

func TestAPIChanged(t *testing.T) {
	tests := []struct {
		name   string
		report string
		want   bool
	}{
		{"empty report", "", false},
		{"compatible", "Compatible changes:\n- Thing: added", true},
		{"incompatible", "Incompatible changes:\n- Thing: removed", true},
		{"prose that is not a report", "no changes found", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := apiChanged(tt.report); got != tt.want {
				t.Errorf("apiChanged(%q) = %v, want %v", strings.TrimSpace(tt.report), got, tt.want)
			}
		})
	}
}
