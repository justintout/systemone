# Contributing

Thanks for taking a look. Pull requests go to `main`.

## Before you open a pull request

```sh
go test -race ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run ./...
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

CI runs the same checks on Go 1.24 and current stable.

A few conventions from the code:

- No dependencies outside the standard library.
- No fallback paths, version shims, or defaults that paper over missing
  configuration. Validation and error handling are not fallbacks.
- Comment why, not what.
- Test behavior at the boundary of a unit, table-driven where it fits.
  Fewer tests that each earn their place.

## Versioning

**This module stays on v0 permanently.** There will never be a v1.0.0.

Go requires a new module path for v2 and beyond, so `github.com/justintout/systemone`
would have to become `github.com/justintout/systemone/v2` to ship a breaking
change after v1. Staying on v0 avoids that split for good. Go already treats v0
as making no compatibility promise, which is the contract this module adopts
deliberately:

| Bump | Means |
| --- | --- |
| `v0.X.0` minor | The exported API changed, or behavior a caller could rely on changed. May break you. |
| `v0.0.X` patch | Neither. Safe to take without reading anything. |

"Behavior a caller could rely on" is the distinction that matters, and it is not
the same as "behavior changed." Fixing a bug changes behavior from wrong to
right, and nobody's correct usage depended on the wrong result — that is a
patch. Changing what `DefaultRetry` returns, or what a zero `Confidence` means,
changes behavior that was already correct, so existing callers have to care —
that is a minor.

Nothing about versioning is asked of a contributor pull request. The bump is
worked out once, when a release is prepared, by comparing the last released tag
against `main`.

### Releases

A release is a tag. Go resolves a module version straight from the tag, so
pushing `v0.2.0` publishes it, and `proxy.golang.org` caches a published
version permanently with no unpublish. A tag cannot be taken back, only
superseded and `retract`ed.

So the tag is produced by the release, not the thing that starts it. A
maintainer starts the `Release` workflow by hand from the Actions tab, on
`main`, choosing `patch` or `minor` — the bump the `Version advice` run for
that commit suggested. The workflow runs the full CI suite first and only
creates and pushes the tag if every job passes, then publishes the GitHub
release and asks the proxy to index it.

Nothing in the source needs editing. The version reported in the `User-Agent`
header is read from the build info at run time, so it is whatever the consumer
resolved and cannot drift from the tag.

**Do not push a `v*` tag by hand.** The tag is the publish, so a hand-pushed
one puts a version on the proxy that no check has seen. The `release tags`
ruleset prevents `v*` tags being updated or deleted, but it cannot prevent them
being created without also blocking the release workflow, so this part is
convention.

### Version advice

Every push to `main` runs the `Version advice` workflow, which works out what
the next tag should be and writes it to the run's job summary. It is advisory:
it does not tag, push or open anything.

It compares the last released tag against `main`:

- `apidiff` reports what changed in the exported API. That is decided from type
  information, not inferred, and any change means at least a minor.
- What is left over goes to `internal/releasebot`, which asks this SDK's own
  client two questions: whether the changes reach a consumer of the module at
  all, and whether any of them alters behavior an existing caller could have
  relied on. The answers are composed in code, in `compose`, where the
  thresholds are readable and tested.

The thresholds lean toward minor. Publishing a breaking change as a patch is
the costlier mistake, since it cannot be withdrawn.
