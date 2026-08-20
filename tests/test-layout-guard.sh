#!/usr/bin/env bash
#
# The layout the suite is required to have, checked by command rather than by
# review. Four checks, in the order they matter.
#
# The first one is the reason the rest exist. `go test` runs a file only when
# its name ends in _test.go: a file named BrokerTest.go compiles into the
# package as ordinary code and every test inside it is skipped, with no error,
# no warning and a green build. Nothing else here can fail that quietly.
set -uo pipefail

cd "$(dirname "$0")/.." || exit 1

fail=0

if git ls-files | grep -E '[A-Za-z]Test\.go$' | grep -v '_test\.go$'; then
	echo "[FAILED] *Test.go is not run by go test"
	fail=1
fi

# A test that only uses the exported API belongs under tests/. One that reads an
# unexported identifier cannot live there at all, so it stays beside the code it
# reads and says so in its name.
#
# The pattern anchors on a path element and not on the start of the path: a
# nested module keeps its own tests/, and `^tests/` would report every file in
# it as misplaced.
if git ls-files '*_test.go' | grep -vE '(^|/)tests/' | grep -v '_internal_test\.go$'; then
	echo "[FAILED] test outside tests/ without _internal suffix"
	fail=1
fi

# The directories are capitalized; the package clause is not. A capitalized
# import name is legal and nobody writes one.
if grep -rn '^package [A-Z]' --include='*.go' tests/ 2>/dev/null; then
	echo "[FAILED] capitalized package clause"
	fail=1
fi

# What ships must not reach the scaffolding. The question is asked of the
# packages that ship and not of ./..., because that pattern matches
# tests/Helpers itself and `go list -deps` prints the packages it was named
# along with everything they import -- so asking it about ./... answers "is
# tests/Helpers part of this module", which it always is.
module=$(GOWORK=off go list -m 2>/dev/null)
shipped=$(GOWORK=off go list ./... 2>/dev/null | grep -v "^${module}/tests/")
if [ -n "$shipped" ]; then
	# shellcheck disable=SC2086 # one package per word is the argument form.
	if GOWORK=off go list -deps $shipped 2>/dev/null | grep -q '/tests/Helpers'; then
		echo "[FAILED] tests/Helpers reached by a binary"
		fail=1
	fi
fi

exit $fail
