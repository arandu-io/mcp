# Contributing

## Sign your commits

Every commit needs a `Signed-off-by` line:

```
git commit -s -m "what changed and why"
```

That line is the [Developer Certificate of Origin](https://developercertificate.org/):
you are stating that you wrote the change, or that you have the right to submit
it under this project's license. It is not a copyright assignment — you keep
your copyright, and this project can never be relicensed behind your back.

We use DCO rather than a CLA on purpose. A CLA would let the project relicense
later, and the price is that every contributor has to sign a legal document
before their first patch.

## Before you open a pull request

```
gofmt -l $(find . -name '*.go' -not -path '*/testdata/*' -not -name '*.kyse.go')
go vet -tags integration,e2e ./...
go test -tags integration,e2e -race ./...
bash tests/test-layout-guard.sh
```

The tags are what the layout guard asks with, and the two CI steps above ask
with them too. Nothing here carries a build tag today, so they change no result
-- they are here so that the day a suite goes behind one, this file is not
telling you to run less than CI runs.

CI runs these four and three more. It checks that no dependency beyond the
framework entered this module: it is imported by applications that expose
themselves to an assistant, and a second require is a download for every one of
them. A pull request that adds one needs to argue for it first, in an issue. It
checks that no Node file appeared. And it runs govulncheck, which is why a
release can be held by a vulnerability in a dependency you did not add.

## Where a test goes

Under `tests/`, in the directory that says what kind of test it is:

| directory | what it holds |
|---|---|
| `tests/Unit/` | one unit on its own: no protocol, no transport |
| `tests/Feature/` | a whole behaviour, crossing layers |
| `tests/Fuzz/` | the fuzz targets, each with its corpus under `tests/Fuzz/testdata/fuzz/<target>/` |
| `tests/Helpers/` | the fakes and servers the tests drive. Ordinary Go, not `*_test.go` |

The directory names the category, so the file name does not repeat it:
`tests/Unit/server_test.go`, never `tests/Unit/server_unit_test.go`. The
directories are capitalized and the package clause is not.

There is one exception, and it is technical rather than a matter of taste: a test
that reads an identifier the package does not export cannot live anywhere but
beside the code it reads. That one is named `<file>_internal_test.go` and stays
put. This module has none -- and a test that stays beside the code without using
anything unexported is the case that needs an argument, not the other way round.
`plans/testpackages.go` in the arandu-io working tree checks exactly that, by
intersecting the identifiers a test names with what its package declares
unexported, and the checklist runs it across every Go repository in the project.

Coverage is measured with `-coverpkg=./...`. Without it, running a tree of tests
reports the coverage of the test packages themselves, which is near zero, and
the number reads like the suite broke.

`tests/test-layout-guard.sh` checks all of the above. The first thing it checks
is the one that fails silently: `go test` runs a file only when its name ends in
`_test.go`, so a file named `ServerTest.go` compiles into the package as
ordinary code and every test inside it is skipped, with a green build.

## What the commit message says

What changed and why. The why is the part that is not in the diff, and it is the
part someone will need in two years.

No AI attribution of any kind: no `Co-Authored-By` for an assistant, no
"generated with" footer. Commits are authored by the people who submit them.

## Architecture decisions

The decisions this project has already made live in `arandu-io/docs`, and every
one that closed a door has an ADR. If your change contradicts one, say so in the
pull request and argue for the change of decision — that is a normal thing to
do, and it is better than a patch that quietly works around it.
