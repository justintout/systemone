# Rulesets

GitHub does not read these files; they are the source of truth for what was
applied, so a change to branch or tag protection shows up in review like any
other change.

List the current rulesets and their ids:

```sh
gh api repos/justintout/systemone/rulesets
```

Apply or update one:

```sh
# create
gh api --method POST repos/justintout/systemone/rulesets --input .github/rulesets/main.json

# update an existing ruleset, by id from `gh api repos/justintout/systemone/rulesets`
gh api --method PUT repos/justintout/systemone/rulesets/RULESET_ID --input .github/rulesets/main.json
```

`main.json` holds `"active"`, which is the intended state. It was first created
`"disabled"`, because an active pull request rule blocks the push that creates
`main` in the first place. Once `main` exists, apply this file as written to
turn it on:

```sh
id=$(gh api repos/justintout/systemone/rulesets --jq '.[] | select(.name=="main") | .id')
gh api --method PUT "repos/justintout/systemone/rulesets/${id}" --input .github/rulesets/main.json
```

Until that runs, `main` has no required pull request, no required checks and no
linear-history enforcement.

Evaluate mode, which logs what a rule would have blocked without blocking it,
needs an Enterprise plan and is not available on this one.

`tags.json` restricts updates and deletions of `v*` tags. Deletion is the one
that matters: `proxy.golang.org` keeps a published version forever, so deleting
a tag does not unpublish anything, it only makes this repository and the proxy
disagree about what that version contains.

Creation is **not** restricted, and cannot usefully be. A `v*` tag should only
ever be created by `release.yml`, after CI has passed, because the tag is the
publish: a hand-pushed tag puts a version on the proxy that no check ever saw,
and no later failure can withdraw it. Enforcing that needs GitHub Actions as a
bypass actor, which a user-owned repository cannot have — the API rejects it
with "Actor GitHub Actions integration must be part of the ruleset source or
owner organization" — and a `creation` rule with no bypass would block the
release workflow's own push.

So this one is convention, held by the fact that only the maintainer has write
access. Moving the repository into an organization, or pushing the tag with a
GitHub App token, would make it enforceable.
