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

The startup log records which upstream was chosen as `Upstream: ...`. When neither
can serve, the line reads `No usable upstream, so every request will be refused` and
the process **still starts**. That is deliberate: an empty account pool is the state a
fresh deployment begins in, and it is repaired by adding an account through the admin
panel that this same process serves, so exiting would remove the only way to fix it.
Read that warning as a failed deploy even though the container is up.

Requests fail closed while it persists: each is refused with a 503, and `/status`
reports zero capacity, so the outage is visible rather than silently served through an
upstream nobody selected.

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
rewritten to a served model if a client sends one anyway — they are what an OpenAI SDK
sends by default, and forwarding them unchanged would 404 after the request had already
been authenticated and held against the wallet. `auto` is **not** rewritten: the
upstream publishes its own `auto` route, and replacing it with a fixed model would strip
a capability the caller asked for.

### Cost basis, measured per model

Measured against the live upstream on 2026-08-06 with `scripts/measure_model_cost.py`,
which solves `billed = overhead + rate * size` across three request sizes. Every model
the pool serves came back between **65.9% and 66.6% margin**, so the single
2,097 VND/1M basis holds across the catalogue and no model needs its own rate.

| model | injected prompt | margin |
|---|---|---|
| claude-haiku-4-5, claude-opus-4-6 | ~2,050 tokens | 65.9-66.1% |
| claude-sonnet-5, claude-opus-4.8 | ~2,200 tokens | 66.1% |
| claude-sonnet-4-6 | ~2,557 tokens | 66.5% |
| claude-haiku-4.5, claude-sonnet-4.6/4.7 | ~2,600 tokens | 66.2-66.5% |
| claude-opus-5, claude-opus-4.6/4-7/4-8 | ~2,623 tokens | 66.4-66.6% |

The basis was previously assumed rather than checked: 0018 measured it on Claude
traffic and every other model inherited it through `*`. The assumption turned out to be
correct, but it had never been tested, and the same reasoning had left eleven models
priced off the fallback row. Migration 0020 gave each of them its own row.

Two spellings of one version (`claude-opus-4.6` and `claude-opus-4-6`) are published
separately upstream and measure differently -- 2,623 tokens of injected prompt against
2,050 -- so they are distinct backends behind equivalent names, and both are priced.

**Re-measure when the pool changes.** A model added upstream is sold immediately, since
the catalogue is read from the upstream rather than from a list here, so it reaches
customers before anyone has priced it. Run the script and add a row.

`auto` is excluded from sale: the upstream picks the model per request, so a call billed
at one rate may have been routed to a model that costs another.

Traffic on this path reports `credits=0`, because the OpenAI protocol carries no
credit meter. `napkey-core` prices it from token counts against `model_prices`, so
every model on sale needs a row there before enabling 9Router. A successful request
that reports neither credits nor tokens is refused rather than stored as free, and
`/v1/admin/usage-audit` lists anything served without a price.

### Verifying the catalogue actually serves

Pricing a model states that NapKey will serve it. Two ids in this catalogue did not:
`claude-sonnet-4.8` returned no usable response and `gpt-image-2` is an image model the
chat endpoint cannot serve. Both were advertised for weeks, and each customer who tried
one paid for a request that was authenticated and held against their wallet before the
upstream refused it. They are now filtered out by `nineRouterUnservable`.

`scripts/check_model_health.py` catches this class of fault. It sends several
concurrent requests per model -- concurrency being the point, since a model that answers
one probe can still fail when requests land together -- and separates three outcomes: a
model that never answers, one that answers intermittently, and one that is merely slow.
It exits non-zero when a priced model cannot serve at all.

```
python3 scripts/check_model_health.py --requests 6 --concurrency 3
```

Run it after any change to the pool, and before promoting a newly published model.

### The upstream injects a prompt into every request

Measured on the live endpoint: a one-word prompt is billed as ~4,541 input tokens, and a
prompt 500 words longer is billed 498 tokens more. The increment tracks the caller's text
exactly, so the remainder is a fixed block the upstream prepends and then charges for.

It is not NapKey's doing. The translator copies the customer's `system` field and nothing
else, no prompt filter runs on the 9Router path, and both `/v1/messages` and
`/v1/chat/completions` report the same counts.

**Customers pay it.** The upstream bills us for those tokens, so absorbing them would
sell below cost. Margin is unharmed, but a customer who sends "Hi" is billed for ~4,500
input tokens, and support cannot tell an inflated bill from a legitimate one by eye. So
each request logs the gap at info level:

```
[9Router] upstream prompt overhead for claude-sonnet-5: billed 4552 input tokens,
caller sent ~11, overhead ~4541 (100% of the charge)
```

The figure is derived per request rather than hardcoded, because it is the upstream's
prompt and can change without notice. Watch it for two things: a sudden jump means the
upstream changed its prompt and every bill moved with it, and the line disappearing means
the overhead is gone and the pricing assumption should be revisited.

Two open questions worth resolving before volume grows: whether the upstream can be asked
to stop injecting it, and whether the overhead should be disclosed in published pricing.

### The upstream ignores max_tokens

Measured on 2026-08-06: a request capped at `max_tokens: 600` came back with 751 to
1,431 output tokens depending on the model. The cap is advisory at best and absent at
worst.

This costs the customer, not margin -- output bills at the same rate as input, so the
overage is charged to whoever set the limit. A caller who sets a budget to bound their
spend does not actually get one.

NapKey cannot fix it from here. Truncating the response would mean charging for tokens
the customer never receives, which is worse than delivering tokens they did not ask
for. So the overshoot is logged instead, at warn level rather than info, because unlike
the injected prompt this breaks a contract the caller believes they have:

```
[9Router] upstream ignored max_tokens for claude-opus-5: caller allowed 600 output
tokens, upstream billed 1431 (139% over); the customer pays the difference
```

Only a response more than 25% past its budget is logged; a few tokens over is the two
tokenisers disagreeing, not a cap that failed. A caller who set no `max_tokens` cannot
have one exceeded, which is the common case on the OpenAI path.

**Where the cap is lost.** Traced through every layer on 2026-08-06: `kiro-go` forwards
`max_tokens` unchanged (both request rewriters edit a generic map precisely so unmodelled
fields survive), Viberouter spreads the body through `NineRouterAdapter`, and vibegateway
at `/opt/vibegateway` validates the field in `protocols/chat-completions.ts` but never
translates it into an output limit -- it does not appear in `upstream.ts` at all. The
provider behind it ignores it. Every layer we own is correct, so there is nothing to fix
in this repo.

Three field names were tried against the live endpoint, all with a limit of 100:
`max_tokens` returned 1,121 tokens, `max_completion_tokens` 757, `max_output_tokens` 875.
So it is not a naming mismatch either.

**It is published rather than hidden.** The docs page carries a "Limitations" section
stating that `max_tokens` is accepted but not guaranteed, with the measured figures, and
points customers at per-key limits for a hard spend ceiling. A customer who caps output
at 600 and is billed for 1,431 is right to ask why; the answer should already be on the
page before they ask. Grep this log line for their request when they do.
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
the open period and inserts a successor. Never edit a row in place — settled usage is
priced against it, and `usage_records.request_fee_micros` is frozen at insert time.

## Incident priorities

- Critical: wallet drift, expired open holds, stuck payment processing, usage reports dropped.
- Warning: unmatched Casso payments, key sync failures, upstream capacity low.
  Capacity is only "low" when there is something to be low against: a pool down to its
  last account, or a quarter of a pool of four or more. A single 9Router upstream reports
  one link, which is full capacity, not a warning -- it is either serving or it is an
  outage. Treat `upstream_capacity_empty` as the incident for that path.
- Warning: `[9Router] no usage reported` in the data plane log means traffic was served but could not be priced.
- Never auto-correct wallet drift. Investigate and post an audited adjustment if required.
