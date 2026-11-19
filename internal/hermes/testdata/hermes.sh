#!/bin/sh
printf '%s|' "$@" > "$ARGS_FILE"
printf '\n' >> "$ARGS_FILE"
if [ -n "$FAKE_SLEEP" ]; then
  sleep "$FAKE_SLEEP"
fi
printf '%s' "$FAKE_OUTPUT"
printf 'session_id: %s\n' "${FAKE_SESSION:-s1}" >&2
exit "${FAKE_EXIT:-0}"
