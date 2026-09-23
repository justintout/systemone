// Command releasebot works out whether the changes since the last release are
// worth publishing, and whether they may break existing callers.
//
// The exported API delta is decided by apidiff, which reads type information
// and is not a judgment. What is left over is: whether a diff affects
// consumers at all, and whether it changes behavior a caller could rely on
// without changing a signature. Neither is visible to static analysis, so both
// go to a System One model, and this package composes the answers in code.
//
// It writes release, bump and reason to GITHUB_OUTPUT.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/justintout/systemone"
)

// Thresholds are deliberately asymmetric. Under this module's permanent-v0
// policy a minor may break callers and a patch may not, so a missed promotion
// ships a breaking change as a patch, while a spurious one costs a version
// number. The bias runs toward minor.
const (
	releaseWorthyAt  = 0.5
	contractChangeAt = 0.35
)

// maxDiffBytes keeps the state inside the model's context budget. Jev takes 32k
// tokens for the state plus the longest question.
const maxDiffBytes = 40_000

var (
	releaseWorthy = systemone.NewNoul("release_worthy",
		map[string]any{
			"question": "Do these changes alter what someone gets when they upgrade this Go library?",
			"focus":    "Judge the effect on a consumer of the published module, not the size or quality of the change.",
		},
		systemone.Yes(map[string]any{
			"what": "Library code, exported documentation, or module metadata changed",
			"examples": []string{
				"A bug fixed in a request path",
				"A doc comment rewritten, which ships in the package documentation",
			},
		}),
		systemone.No(map[string]any{
			"what": "Only repository scaffolding changed, which consumers never receive",
			"examples": []string{
				"CI workflow edits",
				"Test-only changes",
				"README or CONTRIBUTING edits",
			},
		}),
	)

	contractChange = systemone.NewNoul("contract_change",
		map[string]any{
			"question": "Do these changes alter behavior an existing caller could reasonably have relied on?",
			"focus": "A fix that changes behavior from wrong to right is not a contract change, " +
				"because correct usage never depended on the wrong result. A change to behavior " +
				"that was already correct is one, because working code now does something different.",
		},
		systemone.Yes(map[string]any{
			"what": "Correct existing usage now observes something different",
			"examples": []string{
				"A default value changed",
				"The meaning of a returned field changed",
				"A request or response encoding changed",
				"An error that was returned is no longer returned, or vice versa",
			},
		}),
		systemone.No(map[string]any{
			"what": "No working usage changes behavior",
			"examples": []string{
				"A bug fixed so the documented behavior finally holds",
				"New API added alongside the old",
				"An internal refactor with no observable effect",
			},
		}),
	)
)

func main() {
	var (
		baseTag  = flag.String("base", "", "the last released tag")
		diffPath = flag.String("diff", "", "file holding the diff since the base tag")
		apiPath  = flag.String("apidiff", "", "file holding the apidiff report")
		logPath  = flag.String("log", "", "file holding the commit subjects since the base tag")
	)
	flag.Parse()

	if *baseTag == "" || *diffPath == "" || *apiPath == "" || *logPath == "" {
		log.Fatal("releasebot: -base, -diff, -apidiff and -log are all required")
	}

	apiReport := strings.TrimSpace(read(*apiPath))
	decision, err := decide(context.Background(), *baseTag, read(*diffPath), apiReport, read(*logPath))
	if err != nil {
		log.Fatalf("releasebot: %v", err)
	}
	if err := emit(decision); err != nil {
		log.Fatalf("releasebot: %v", err)
	}
	fmt.Printf("release=%t bump=%s\n%s\n", decision.Release, decision.Bump, decision.Reason)
}

type decision struct {
	Release bool
	Bump    string // "minor" or "patch"
	Reason  string
}

// apiChanged reports whether apidiff found any exported API difference. Its
// report is empty when there is none.
func apiChanged(report string) bool {
	return strings.Contains(report, "Incompatible changes:") ||
		strings.Contains(report, "Compatible changes:")
}

func decide(ctx context.Context, baseTag, diff, apiReport, commits string) (decision, error) {
	client, err := systemone.New(systemone.WithRetry(systemone.DefaultRetry()))
	if err != nil {
		return decision{}, err
	}

	if len(diff) > maxDiffBytes {
		// Trim back to a rune boundary; a split rune would reach the model as
		// U+FFFD.
		cut := maxDiffBytes
		for cut > 0 && !utf8.RuneStart(diff[cut]) {
			cut--
		}
		diff = diff[:cut] + "\n... diff truncated ...\n"
	}

	// The state names each part so the questions can point at it. apidiff's
	// report goes in as evidence: the model does not decide the API delta, but
	// knowing it helps it judge behavior.
	state := map[string]any{
		"library":        "A Go client library for the TypeSafe System One HTTP API.",
		"last_release":   baseTag,
		"commits":        subjects(commits),
		"exported_api":   exported(apiReport),
		"diff_since_tag": diff,
	}

	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	res, err := client.Ask(ctx, state, releaseWorthy, contractChange)
	if err != nil {
		return decision{}, err
	}
	worth, err := releaseWorthy.From(res)
	if err != nil {
		return decision{}, err
	}
	contract, err := contractChange.From(res)
	if err != nil {
		return decision{}, err
	}

	return compose(apiReport, worth, contract), nil
}

// compose turns the two answers and the apidiff report into a decision. It is
// separate from the request so the policy can be read, and tested, on its own.
// Changing a threshold here needs no new inference.
func compose(apiReport string, worth, contract systemone.NoulAnswer) decision {
	switch {
	case apiChanged(apiReport):
		return decision{true, "minor", fmt.Sprintf(
			"The exported API changed, so this is at least a minor.\n\n```\n%s\n```", apiReport)}

	case !worth.Yes(releaseWorthyAt):
		return decision{false, "", fmt.Sprintf(
			"Nothing here reaches a consumer of the module (p=%.2f), so no release.", worth.Value)}

	case contract.Yes(contractChangeAt):
		return decision{true, "minor", fmt.Sprintf(
			"The exported API is unchanged, but this looks like it alters behavior an existing "+
				"caller could have relied on (p=%.2f), which is a minor under this module's "+
				"versioning policy.", contract.Value)}

	default:
		return decision{true, "patch", fmt.Sprintf(
			"The exported API is unchanged and behavior existing callers rely on looks intact "+
				"(p=%.2f), so this is a patch.", contract.Value)}
	}
}

// subjects splits commit subjects into a list. strings.Split would turn empty
// input into a one-element slice holding "", which reads as one commit with a
// blank subject rather than as no commits.
func subjects(commits string) []string {
	commits = strings.TrimSpace(commits)
	if commits == "" {
		return []string{}
	}
	return strings.Split(commits, "\n")
}

func exported(report string) string {
	if report == "" {
		return "unchanged"
	}
	return report
}

func emit(d decision) error {
	path := os.Getenv("GITHUB_OUTPUT")
	if path == "" {
		return nil // running locally
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = fmt.Fprintf(f, "release=%t\nbump=%s\nreason<<REASON_EOF\n%s\nREASON_EOF\n",
		d.Release, d.Bump, d.Reason)
	return err
}

func read(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("releasebot: %v", err)
	}
	return string(data)
}
