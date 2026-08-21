#!/usr/bin/env bash
#
# The layout the suite is required to have, checked by command rather than by
# review. Four checks, in the order they matter.
#
# The first one is the reason the rest exist. `go test` runs a file only when
# its name ends in _test.go: a file named BrokerTest.go compiles into the
# package as ordinary code and every test inside it is skipped, with no error,
# no warning and a green build. Nothing else here can fail that quietly.
#
# Nothing below may pass by having nothing to look at. Every check here is a
# statement about a set of files, and every one of them is true of the empty
# set -- so a guard pointed at a tree holding none reports success having read
# none, and a log cannot tell that green from the other one. A question the
# toolchain could not answer is the same thing said differently: `go list`
# failing is a check that did not run, not a check that passed.
set -uo pipefail

cd "$(dirname "$0")/.." || exit 1

fail=0

# The two sets every check below is about, counted before any of them runs.
sources=$(git ls-files '*.go' | grep -c '')
suite=$(git ls-files '*_test.go' | grep -c '')

if [ "$sources" -eq 0 ] || [ "$suite" -eq 0 ]; then
	echo "[FAILED] $sources Go files and $suite test files are tracked here."
	echo "         Every check below is true of nothing, so none of them ran."
	exit 1
fi

# 1. A test file the toolchain does not recognise as one.
#
#    The pattern is Tests?\.go with a capital T, which is every hand of the same
#    mistake -- BrokerTest.go, broker_Test.go, BrokerTests.go, Test.go -- and no
#    false positive: latest.go ends in a lowercase test.go, and so does a real
#    test file. Requiring a letter in front of Test, which is what stood here,
#    read broker_Test.go as correct and let a file full of tests sit at the root
#    of the module with nothing running it.
offenders=$(git ls-files '*.go' | grep -E 'Tests?\.go$')
if [ -n "$offenders" ]; then
	echo "[FAILED] go test does not run these, and will not say so:"
	printf '%s\n' "$offenders" | sed 's/^/    /'
	fail=1
fi

# 2. A test that only uses the exported API belongs under tests/. One that reads
#    an unexported identifier cannot live there at all, so it stays beside the
#    code it reads and says so in its name.
#
#    The pattern anchors on a path element and not on the start of the path: a
#    nested module keeps its own tests/, and `^tests/` would report every file
#    in it as misplaced.
misplaced=$(git ls-files '*_test.go' | grep -vE '(^|/)tests/' | grep -v '_internal_test\.go$')
if [ -n "$misplaced" ]; then
	echo "[FAILED] test outside tests/ without _internal suffix:"
	printf '%s\n' "$misplaced" | sed 's/^/    /'
	fail=1
fi

# 3. The directories are capitalized; the package clause is not. A capitalized
#    import name is legal and nobody writes one.
#
#    The files are named from what git tracks rather than walked with grep -r,
#    because grep -r over a directory that is not there fails and says nothing:
#    the check that read no package clause at all looked exactly like the check
#    that read them and approved.
inspected=$(git ls-files '*.go' | grep -E '(^|/)tests/')
if [ -z "$inspected" ]; then
	echo "[FAILED] no Go file is tracked under tests/, so no package clause was read"
	fail=1
else
	clauses=$(printf '%s\n' "$inspected" | xargs grep -n '^package [A-Z]')
	if [ -n "$clauses" ]; then
		echo "[FAILED] capitalized package clause:"
		printf '%s\n' "$clauses" | sed 's/^/    /'
		fail=1
	fi
fi

# 4. What ships must not reach the scaffolding. It imports testing, and a
#    package that reaches it registers the flags of a test binary into whatever
#    imports it.
#
#    The question is asked of the packages that ship and not of ./..., because
#    that pattern also lists the test packages themselves and every one of those
#    reaches the tests tree, which is what it is for.
#
#    Both answers from the toolchain are checked. Asking ./... about a module
#    where the violation is already present fails -- tests/Helpers imports this
#    package, so anything shipped that imports Helpers is an import cycle -- and
#    the shape that stood here read that failure as an empty list of packages
#    and skipped the check. The one condition the check exists to catch was the
#    one condition under which it could not run.
#
#    The tags are passed because a suite behind one is invisible without them,
#    and so is a package that ever grows a build tag of its own.
if ! packages=$(GOWORK=off go list -tags 'integration e2e' ./... 2>&1); then
	echo "[FAILED] go list did not answer, so nothing here was checked:"
	printf '%s\n' "$packages" | sed 's/^/    /'
	fail=1
else
	shipped=$(printf '%s\n' "$packages" | grep -vE '/tests(/|$)')
	if [ -z "$shipped" ]; then
		echo "[FAILED] no package outside tests/ was listed, so the rule was asked about nothing"
		fail=1
	else
		# One import path per line and none of them has a space: the split is
		# wanted. The directive sits in front of the whole compound rather than
		# in front of a branch of it, because shellcheck refuses one attached to
		# an elif -- and refusing it is not a warning, it is a parse error that
		# stops the file being read at all. A guard nothing can lint is the
		# thing this guard exists to forbid, one level up.
		# shellcheck disable=SC2086
		if ! dependencies=$(GOWORK=off go list -tags 'integration e2e' -deps $shipped 2>&1); then
			echo "[FAILED] go list -deps did not answer, so nothing here was checked:"
			printf '%s\n' "$dependencies" | sed 's/^/    /'
			fail=1
		else
			reached=$(printf '%s\n' "$dependencies" | grep -E '/tests(/|$)')
			if [ -n "$reached" ]; then
				echo "[FAILED] a package that ships reaches the tests tree:"
				printf '%s\n' "$reached" | sed 's/^/    /'
				fail=1
			fi
		fi
	fi
fi

exit $fail
