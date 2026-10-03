#!/usr/bin/env bash
# Fails when a hand-written Go file is too long (see CLAUDE.md "Code size limits").
# Generated files (with the standard "Code generated ... DO NOT EDIT." header) are exempt.
set -euo pipefail

MAX_GO="${MAX_GO:-300}"
MAX_TEST="${MAX_TEST:-500}"

cd "$(git rev-parse --show-toplevel)"

status=0
while IFS= read -r -d '' file; do
	if head -n 5 "$file" | grep -qE '^// Code generated .* DO NOT EDIT\.$'; then
		continue
	fi
	limit="$MAX_GO"
	[[ "$file" == *_test.go ]] && limit="$MAX_TEST"
	lines=$(wc -l <"$file")
	if ((lines > limit)); then
		echo "$file: $lines lines (limit $limit). Split it by responsibility." >&2
		status=1
	fi
done < <(git ls-files -z --cached --others --exclude-standard -- '*.go')

exit "$status"
