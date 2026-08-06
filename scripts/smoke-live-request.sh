#!/usr/bin/env bash
# Serve one real request through the whole chain and prove the money moved correctly.
#
# Every check below exists because something that passed in isolation still broke in
# production. The hold shortfall found on 2026-08-06 is the example: unit tests passed,
# the price book was right, and money still went unbilled -- because the hold was sized
# from what the caller declared while settlement used what the upstream reported. Only
# a real request with real token counts shows that gap, so this script measures the
# wallet before and after rather than trusting either side's arithmetic.
#
# Run on the server (it needs docker exec). Pass a live customer API key:
#
#   ./scripts/smoke-live-request.sh nk_live_xxx [model]
#
# Exit status is 0 only when every check passes, so it can gate a deploy.
set -uo pipefail

KEY="${1:-}"
MODEL="${2:-claude-sonnet-5}"
if [ -z "$KEY" ]; then
  echo "usage: $0 <customer-api-key> [model]" >&2
  exit 2
fi

KIRO=$(docker ps -qf name=kiro-go | head -1)
PG=$(docker ps -qf name=postgres | head -1)   # the host runs several; pin to the first
if [ -z "$KIRO" ] || [ -z "$PG" ]; then
  echo "FAIL: kiro-go or postgres container not found" >&2
  exit 1
fi

psql_q() {
  docker exec -e PGPASSWORD="$(docker exec "$PG" printenv POSTGRES_PASSWORD)" "$PG" \
    psql -tAq -U "$(docker exec "$PG" printenv POSTGRES_USER)" \
         -d "$(docker exec "$PG" printenv POSTGRES_DB)" -c "$1"
}

FAILURES=0
check() { # check <description> <condition-result> <detail>
  if [ "$2" = "ok" ]; then printf '  PASS  %s\n' "$1"
  else printf '  FAIL  %s -- %s\n' "$1" "$3"; FAILURES=$((FAILURES + 1)); fi
}

KEY_PREFIX=$(printf '%s' "$KEY" | cut -c1-16)
USER_ID=$(psql_q "SELECT user_id FROM api_keys WHERE key_prefix LIKE '${KEY_PREFIX}%' AND revoked_at IS NULL LIMIT 1;" | tr -d '[:space:]')
if [ -z "$USER_ID" ]; then
  echo "FAIL: no live api_keys row matches this key's prefix; was it revoked?" >&2
  exit 1
fi

BALANCE_BEFORE=$(psql_q "SELECT balance_micros FROM wallets WHERE user_id = '$USER_ID';" | tr -d '[:space:]')
RECORDS_BEFORE=$(psql_q "SELECT count(*) FROM usage_records WHERE user_id = '$USER_ID';" | tr -d '[:space:]')
echo "customer $USER_ID, balance $((BALANCE_BEFORE / 1000000)) VND, $RECORDS_BEFORE usage records"
echo "serving one real '$MODEL' request through kiro-go..."

BODY=$(printf '{"model":"%s","messages":[{"role":"user","content":"Reply with exactly one word: ready"}],"max_tokens":64}' "$MODEL")
RESPONSE=$(docker exec "$KIRO" wget -qO- \
  --header="Authorization: Bearer $KEY" --header="Content-Type: application/json" \
  --post-data="$BODY" http://localhost:8080/v1/chat/completions 2>&1)
SERVED=$?

# Settlement is asynchronous: kiro-go reports usage after the response is returned.
for _ in $(seq 20); do
  RECORDS_AFTER=$(psql_q "SELECT count(*) FROM usage_records WHERE user_id = '$USER_ID';" | tr -d '[:space:]')
  [ "$RECORDS_AFTER" -gt "$RECORDS_BEFORE" ] && break
  sleep 1
done

BALANCE_AFTER=$(psql_q "SELECT balance_micros FROM wallets WHERE user_id = '$USER_ID';" | tr -d '[:space:]')
read -r CHARGED UPSTREAM_COST IN_TOK OUT_TOK UNPRICED <<<"$(psql_q "
  SELECT cost_micros, upstream_cost_micros, input_tokens, output_tokens, unpriced
    FROM usage_records WHERE user_id = '$USER_ID' ORDER BY created_at DESC LIMIT 1;" \
  | tr '|' ' ')"

echo
echo "results:"
[ "$SERVED" -eq 0 ] && check "the request was served" ok \
  || check "the request was served" bad "wget exited $SERVED: $(printf '%s' "$RESPONSE" | head -c 200)"

[ "${RECORDS_AFTER:-0}" -gt "$RECORDS_BEFORE" ] \
  && check "usage was reported and settled" ok \
  || check "usage was reported and settled" bad "no new usage_records row after 20s; a served request that is never billed is revenue lost silently"

DEBITED=$((BALANCE_BEFORE - BALANCE_AFTER))
[ "$DEBITED" -gt 0 ] && check "the wallet was debited" ok \
  || check "the wallet was debited" bad "balance unchanged at $BALANCE_BEFORE"

# The debit must equal the recorded charge. A mismatch means hold and settlement
# disagree, which is how a request gets served without being paid for.
if [ -n "${CHARGED:-}" ] && [ "$DEBITED" = "$CHARGED" ]; then
  check "the debit equals the recorded charge" ok
else
  check "the debit equals the recorded charge" bad "wallet moved $DEBITED but usage_records says ${CHARGED:-none}"
fi

[ "${UNPRICED:-t}" = "f" ] && check "the model had a price on file" ok \
  || check "the model had a price on file" bad "$MODEL settled unpriced, so it was served for free"

# Every hold must resolve. One left open pins a customer's money for 15 minutes.
STUCK=$(psql_q "SELECT count(*) FROM wallet_holds WHERE status = 'open' AND expires_at < now();" | tr -d '[:space:]')
[ "$STUCK" = "0" ] && check "no wallet holds are stuck open" ok \
  || check "no wallet holds are stuck open" bad "$STUCK hold(s) open past expiry"

if [ -n "${CHARGED:-}" ] && [ -n "${UPSTREAM_COST:-}" ] && [ "$CHARGED" -gt 0 ]; then
  echo
  awk -v r="$CHARGED" -v c="$UPSTREAM_COST" -v i="${IN_TOK:-0}" -v o="${OUT_TOK:-0}" 'BEGIN{
    printf "economics of this request: %d in + %d out tokens, charged %.1f VND, cost %.1f VND, profit %.1f VND (%.1f%% margin)\n",
      i, o, r/1e6, c/1e6, (r-c)/1e6, 100*(r-c)/r }'
fi

echo
if [ "$FAILURES" -eq 0 ]; then echo "all checks passed: the money path works end to end"; exit 0; fi
echo "$FAILURES check(s) failed"; exit 1