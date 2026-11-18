#!/usr/bin/env bash
# Writes the body of a release with cmd/releasenotes (see "Release process" in
# docs/DEVELOPMENT.md). The pull requests merged since the release before come
# from the GitHub API; everything else from git. Used by release.yml (a new
# release) and release-notes.yml (the notes of an existing one).
#
#   .github/scripts/release-notes.sh VERSION REF OUT
#
# VERSION is vX.Y.Z, REF the release's commit or tag, OUT the file to write.
# Needs GH_TOKEN (pull-requests: read), GITHUB_REPOSITORY, Go, jq and the full
# history with tags (actions/checkout with fetch-depth: 0). Fails without
# writing OUT when anything goes wrong.
set -euo pipefail

if [ "$#" -ne 3 ]; then
  echo "usage: $0 VERSION REF OUT" >&2
  exit 2
fi
version="$1" ref="$2" out="$3"
if ! [[ "$version" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
  echo "::error::The version must be vX.Y.Z"
  exit 1
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
go build -o "$work/releasenotes" ./cmd/releasenotes

"$work/releasenotes" -version "$version" -to "$ref" -list-prs > "$work/numbers"
: > "$work/prs.jsonl"
while IFS= read -r n; do
  if ! [[ "$n" =~ ^[1-9][0-9]*$ ]]; then
    echo "::error::releasenotes listed something that is not a pull request number"
    exit 1
  fi
  # A "(#N)" in a commit subject may name an issue: the API answers 404, and
  # the commit is listed without a pull request.
  if ! gh api "repos/$GITHUB_REPOSITORY/pulls/$n" \
    --jq '{number, title, body, user: {login: .user.login}}' >> "$work/prs.jsonl" 2> "$work/err"; then
    if grep -q 'HTTP 404' "$work/err"; then
      echo "::notice::#$n is not a pull request; its commits are listed by themselves."
      continue
    fi
    cat "$work/err" >&2
    echo "::error::Could not read pull request #$n"
    exit 1
  fi
done < "$work/numbers"
jq -s . "$work/prs.jsonl" > "$work/prs.json"
echo "Pull requests: $(jq length "$work/prs.json")"

"$work/releasenotes" -version "$version" -to "$ref" -prs "$work/prs.json" -repo "$GITHUB_REPOSITORY" > "$work/notes.md"
mv "$work/notes.md" "$out"
