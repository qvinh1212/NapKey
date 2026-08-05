# NapKey Operations Runbook

## Release gate

Run from the repository root:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\verify-roadmap.ps1
```

Do not deploy when migrations, Go tests, the Next production build, or the
offline dependency audit fail.

## Database migration and rollback

Migrations are forward-only and run during `napkey-core` startup. Before every
release that contains a migration:

1. Create a compressed Postgres backup.
2. Verify the backup by restoring it into a separate database.
3. Deploy the application.
4. Check `/health`, `/ready`, and the operations dashboard.

Application rollback means redeploying the previous image. Schema rollback is a
database restore; never hand-edit wallet or ledger rows to imitate a rollback.

```powershell
.\scripts\backup-postgres.ps1 -Container napkey-postgres -Database napkey -User napkey
.\scripts\restore-postgres.ps1 -Container napkey-postgres -Database napkey_restore_check -User napkey -BackupFile .\backups\napkey-YYYYMMDD-HHMMSS.dump
```

## Casso go-live acceptance

This requires the real Casso account and cannot be simulated by a local fixture:

1. Configure Webhook V2 with the public HTTPS URL `/webhooks/casso` and Strict mode.
2. Store the webhook secret and API key only in Coolify environment variables.
3. Use Casso **Gọi thử** and retain the exact raw body and signature as the production fixture.
4. Transfer the minimum real amount with the generated memo.
5. Confirm exactly one `payment_events` row, one top-up ledger entry, and the expected wallet balance.
6. Replay the event and confirm the balance does not change.
7. Confirm unmatched and rejected events appear on `/vi/console/admin`.

## Google sign-in

Sign-in with Google is optional. With `GOOGLE_CLIENT_ID` and
`GOOGLE_CLIENT_SECRET` unset the console keeps working on email and password, and
the Google button lands back on the sign-in page with an explanation. Setting only
one of the pair is refused at startup.

1. In Google Cloud Console create an OAuth 2.0 Web application client.
2. Set the authorized redirect URI to `https://napkey.io.vn/api/v1/auth/google/callback`
   exactly. napkey-core derives it from `PUBLIC_BASE_URL`, so a mismatch there
   breaks the callback.
3. Store both values only in Coolify environment variables.
4. After deploying, sign in with an account that has no NapKey user and confirm one
   `oauth_identities` row, a verified user, and the trial grant.
5. Sign in again and confirm no second identity row and no second trial grant.
6. Sign in with a Google account whose email already has a password account: the
   flow must stop on `oauth_error=account_conflict` rather than link silently.

The browser only ever reaches `/api/v1/auth/google/start` and
`/api/v1/auth/google/callback`; the token exchange runs server-side, so the client
secret never leaves the private network.

## Cloudflare

Create a narrowly scoped WAF skip rule for the Casso source IPs and only the
webhook path. Keep SSL mode at **Full (strict)**. Confirm with Casso support that
their request receives HTTP 200 within five seconds; do not disable the WAF for
the whole API hostname.

## 9Router upstream

`kiro-go` serves customer traffic from the 9Router runtime endpoint by default.
`NINEROUTER_ENABLED=false` is the only value that switches back to the local
account pool; any other value keeps 9Router, so a typo cannot silently move
traffic onto a pool that may hold no accounts.

The process refuses to start when neither upstream can serve, so a missing
endpoint or key fails the deploy instead of every request. The startup log line
`Upstream: ...` records which one was chosen.

Check reachability without waiting for a customer request. The admin API is
restricted to the configured admin host, so run this against that hostname or from
inside the network:

```powershell
curl -X POST https://<admin-host>/admin/api/upstream/probe `
  -H "X-Admin-Password: $env:ADMIN_PASSWORD"
```

`ok: true` means the upstream answered. A 503 carries the reason; the endpoint key
is never echoed back. `/status` and `/health` report configured capacity rather
than probing, so the public status page cannot be slowed by a slow upstream.

Two Anthropic features have no OpenAI equivalent and are dropped on this path:
extended thinking, and server tools such as `web_search`. `/v1/responses` returns
501 rather than falling back to the pool the operator switched away from.

### Model pool

The upstream addresses models by provider pool, so `NINEROUTER_MODEL_PREFIX` has to
name a pool that instance actually serves. A wrong prefix fails **every** request with
a 404, not a subset, because the namespaced id does not exist.

Check what the upstream serves before changing it:

```powershell
# `curl` is an alias for Invoke-WebRequest in PowerShell 5.1, so call curl.exe
curl.exe -H "Authorization: Bearer $env:NINEROUTER_API_KEY" `
  https://<upstream-host>/v1/models
```

`NINEROUTER_MODEL_PREFIX` defaults to `Viberouter/`, the pool carrying the full Claude
line including every model this price book has a rate for. Two things to know if you
consider changing it:

- A free tier is a poor choice for resale even when it lists the same models: paying
  customers would sit behind someone else's quota.
- Models with a nested namespace (`vendor/sub/model`) are skipped, because the gateway
  cannot map them back to a public id customers can send.

The retired OpenAI ids (`gpt-4o`, `gpt-4`, `gpt-3.5-turbo`) are not advertised, and are
rewritten to a served model if a client sends one anyway ? they are what an OpenAI SDK
sends by default, and forwarding them unchanged would 404 after the request had already
been authenticated and held against the wallet. `auto` is **not** rewritten: the
upstream publishes its own `auto` route, and replacing it with a fixed model would strip
a capability the caller asked for.

### Cost basis is only measured for Claude

`/v1/models` advertises everything the configured pool serves. On the verified upstream
that is 38 models, while the price book names 5; the other 33 fall through to the `*`
row, which charges the same rate and fee, so **nothing is served free**.

What is not covered is the cost side. The 2,097 VND/1M and 110 VND/call basis was
measured on Claude traffic. The pool also carries non-Claude models ? `gpt-5.6-*`,
`deepseek-3.2`, `glm-5`, `minimax-*`, `qwen3-coder-next` ? whose real upstream cost is
unmeasured. If any of them costs more than the Claude basis, margin reporting will show
roughly 72% while the actual margin is lower.

`auto` has the same gap for a different reason: the upstream picks the model per
request, so a call billed at the flat fallback rate may have been routed to an expensive
model.

Neither is a reason to hold the deploy, but measure per-family cost before promoting
non-Claude models, and give any model sold in volume its own price row rather than
leaving it on `*`.

Traffic on this path reports `credits=0`, because the OpenAI protocol carries no
credit meter. `napkey-core` prices it from token counts against `model_prices`, so
every model on sale needs a row there before enabling 9Router. A successful request
that reports neither credits nor tokens is refused rather than stored as free, and
`/v1/admin/usage-audit` lists anything served without a price.

### Pricing on the token path

Traffic served through 9Router is priced from tokens **plus a flat fee per request**
(migration 0019: 300 VND retail over a measured 110 VND basis, per model and on the
`*` fallback).

The fee is not a markup. The upstream bills roughly one credit per call whatever its
size, and a token-only price cannot see that fixed cost, so small requests settled
below cost:

| request | tokens | tokens only | with fee | upstream cost |
|---|---|---|---|---|
| chat, one line | 350 | 4 VND | 304 VND | 110 VND |
| agent step | 1,800 | 22 VND | 322 VND | 110 VND |
| agent, mid-size | 8,800 | 106 VND | 406 VND | 110 VND |
| large context | 46,200 | 554 VND | 854 VND | 207 VND |

Blended margin is 72.3%, matching the 72.5% the credit path already earns, so
switching upstream does not change what a customer pays for the same work.

Two consequences worth knowing when reading the ledger:

- Credit-metered requests (the Kiro account pool) carry **no** fee. The credit meter
  already prices those end to end, and charging both would bill one fixed cost twice.
  `request_fee_micros` is zero on those rows by design, not by omission.
- The fee raises the wallet hold as well, because the hold is quoted through the same
  pricing function as settlement. An agent-sized request now reserves ~370 VND instead
  of ~67 VND, which is what stops a nearly empty wallet authorising more concurrent
  calls than it can pay for.

Changing the fee is a price change, not a config change: add a migration that closes
the open period and inserts a successor. Never edit a row in place ? settled usage is
priced against it, and `usage_records.request_fee_micros` is frozen at insert time.

## Incident priorities

- Critical: wallet drift, expired open holds, stuck payment processing, usage reports dropped.
- Warning: unmatched Casso payments, key sync failures, pool capacity below one usable account.
- Warning: `[9Router] no usage reported` in the data plane log means traffic was served but could not be priced.
- Never auto-correct wallet drift. Investigate and post an audited adjustment if required.
