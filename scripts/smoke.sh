#!/bin/sh
# 用法: TOKEN=口令 CHANNEL=301 ./scripts/smoke.sh http://127.0.0.1:8787
set -eu
BASE="${1:-http://127.0.0.1:8787}"
CHANNEL="${CHANNEL:-301}"
TOKEN="${TOKEN:-}"

curl -fsS "$BASE/health"
echo
body=$(mktemp)
trap 'rm -f "$body"' EXIT
python3 - "$body" <<'PY'
import sys
open(sys.argv[1], "w", encoding="utf-8").write("烟测：请翻开第12页")
PY

if [ -n "$TOKEN" ]; then
  curl -fsS -X POST "$BASE/api/v1/messages" \
    -H "X-Channel: $CHANNEL" \
    -H "X-Token: $TOKEN" \
    -F "body=<$body"
  echo
  curl -fsS "$BASE/api/v1/messages?channel=$CHANNEL&limit=5" \
    -H "X-Token: $TOKEN"
else
  curl -fsS -X POST "$BASE/api/v1/messages" \
    -H "X-Channel: $CHANNEL" \
    -F "body=<$body"
  echo
  curl -fsS "$BASE/api/v1/messages?channel=$CHANNEL&limit=5"
fi
echo
