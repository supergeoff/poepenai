#!/bin/sh

# Exit immediately if a command exits with a non-zero status.
set -e
# Optional: Print commands and their arguments as they are executed for debugging.
# set -x

COMMIT_MSG_FILE="$1" # The first argument passed to the script

if [ -z "$COMMIT_MSG_FILE" ]; then
  echo "Error: Commit message file argument is empty." >&2
  exit 1
fi

if [ ! -f "$COMMIT_MSG_FILE" ]; then
  echo "Error: Commit message file [$COMMIT_MSG_FILE] not found." >&2
  exit 1
fi

# echo "Debug: Validating file: $COMMIT_MSG_FILE" >&2
# echo "--- Start of content ---" >&2
# cat "$COMMIT_MSG_FILE" >&2
# echo "--- End of content ---" >&2

if grep -Eq '^(feat|fix|docs|style|refactor|perf|test|chore)(\(.+\))?: .{1,72}$' "$COMMIT_MSG_FILE"; then
  echo "✅ Commit message format is valid." >&2 # Optional success message
  exit 0
else
  echo "❌ Commit message format is invalid." >&2
  echo "The first line of your commit message must follow the Conventional Commits format." >&2
  echo "Example: feat(parser): add ability to parse arrays" >&2
  echo "Allowed types: feat, fix, docs, style, refactor, perf, test, chore" >&2
  echo "Full pattern: ^(feat|fix|docs|style|refactor|perf|test|chore)(\\(.+\\))?: .{1,72}$" >&2
  exit 1
fi