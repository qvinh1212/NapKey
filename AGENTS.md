# AGENTS.md — NapKey

Orientation for an agent starting a session here. Everything below was verified against
running containers and source, not inferred from names.

## The names lie; check this table first

| service | role | `NINEROUTER_*` | `DATABASE_URL` |
|---|---|---|---|
| `kiro-go` | data plane: customer auth, upstream calls, usage reporting | **yes** | no |
| `napkey-core` | control plane: wallet, price book, usage ledger, settlement | no | **yes** |
| `postgres` | the database | no | — |
| `napkey-web` | public site and customer console | no | no |

Two traps worth naming, because both have cost real time:

- **`kiro-go` is not the Kiro pool.** It is the proxy that serves every customer request,
  and 9Router is what it calls. All the `NINEROUTER_*` configuration lives here
  (`docker-compose.coolify.yml`).
- **`napkey-core` never touches the upstream.** It owns money and prices. Reach for it
  when the question is about billing, and for `kiro-go` when it is about serving traffic.

## The upstream chain

```
customer -> kiro-go -> 9Router (vibegateway) -> provider
```

Three hops, not four. `viberouter.io.vn` is **just the domain 9Router answers on** — it is
not a service in the path, and `Viberouter/<model>` is a pool name, not a component. An
earlier version of this file listed it as a forwarding hop; that was wrong and it cost a
session chasing a container that does not exist.

- **9Router** — runs on the host, *not* in a container. Source at `/opt/vibegateway`,
  listening on `10.0.1.1:20242`. Locate it with `ss -tlnp | grep 20242`, then read
  `/proc/<pid>/cwd`. Derived from `github.com/decolua/9router` plus a local patch under
  `ops/9router-patches/`. `kiro-go` reaches it through `NINEROUTER_RUNTIME_BASE_URL`.
- **provider** — configured in the 9Router dashboard at `/dashboard/providers`. This is
  the layer that injects a system prompt and ignores `max_tokens`.

Both hops are operated by us. Neither is a third-party service to file a bug with. If a
request behaves oddly, trace it down this chain rather than guessing what sits below.

## Reaching things on the server

```bash
# upstream key -- from the data plane
docker exec $(docker ps -qf name=kiro-go) printenv NINEROUTER_API_KEY

# price book -- pin the container; other projects on this host also run postgres
docker ps --format "{{.ID}}  {{.Names}}" | grep postgres
PG=$(docker ps -qf name=postgres | head -1)
docker exec -e PGPASSWORD="$(docker exec $PG printenv POSTGRES_PASSWORD)" $PG \
  psql -U "$(docker exec $PG printenv POSTGRES_USER)" -d "$(docker exec $PG printenv POSTGRES_DB)" \
  -c "SELECT model, effective_from FROM model_prices WHERE effective_to IS NULL ORDER BY model;"

# the catalogue kiro-go advertises
docker exec $(docker ps -qf name=kiro-go) wget -qO- http://localhost:8080/v1/models
```

Coolify deploys are **manual**: pushing to `origin/main` does not deploy. Someone has to
press Redeploy, so verifying a change on production means confirming the deploy landed
first. `docker exec ... printenv` is the way to read config; environment variables set in
the Coolify UI exist only inside containers, never in a local shell.

## Billing has one path: tokens

Requests are priced from token counts against `model_prices`. There is no second
mechanism, and a usage report that carries a credit meter is now **refused**
(`store.ErrCreditMeteringRetired`).

Do not reintroduce credit-metered billing without measuring the upstream's cost per
call first. The retired version was wrong in a way that hid itself:
`UpstreamVNDPerCredit` held 110, the measured cost of one upstream *call*, but it was
multiplied by the *credit count*, and the Kiro meter reported ~0.124 credits per call.
Requests costing 110 VND were booked at 13.6, and the margin dashboard read a healthy
70% on traffic that was losing money for a month.

Credits still exist as the unit a **wallet top-up** is denominated in
(`RetailVNDPerCredit = 400`). That is a customer-facing label for money held, not a
billing basis. Keep the two apart when reading `usage_records`.

Reading history correctly matters here: the credit path served real traffic until
2026-07-30, so any query over `usage_records` that does not cut by date mixes two
pricing mechanisms and two cost bases. `priced_with IS NULL AND NOT unpriced`
identifies the old credit rows.

## Upstream behaviour that surprises people

Measured 2026-08-06, documented in `docs/OPERATIONS.md`:

- **`max_tokens` is not enforced.** A cap of 100 returned 1,121 output tokens. Every layer
  we own forwards the field correctly; the provider ignores it. Published as a limitation
  on the docs page rather than worked around -- truncating would charge for tokens the
  customer never receives.
- **Every request carries 2,000-2,600 injected tokens.** The upstream prepends its own
  prompt and bills for it. Margin is unaffected because the customer is billed from the
  `prompt_tokens` the upstream reports, so it is resold at retail. It is why a one-line
  chat records as thousands of input tokens.
- **The endpoint always streams.** `text/event-stream` regardless of the `stream` flag,
  with `usage` only on the final chunk. Parsing the body as one JSON object fails.
- **Cloudflare fronts the gateway** and rejects default HTTP agents with error 1010. Send
  a curl-like `User-Agent`.

## Two scripts worth knowing

- `scripts/measure_model_cost.py` — what the upstream really bills per model. Solves
  `billed = overhead + rate * size` across three request sizes, so no client tokeniser is
  needed. There is a PowerShell twin, `scripts/measure-model-cost.ps1`.
- `scripts/check_model_health.py` — whether every priced model can actually serve, under
  concurrency. Exits non-zero when a priced model cannot answer at all.

Both hit the live upstream with real, billable requests on a key shared with production.
Start with one model.

## Conventions

- Go sources are CRLF in this repo. `gofmt -l` flags almost every file for that reason
  alone; check content by copying to a temp file with LF endings before assuming a
  formatting problem.
- `napkey-web/messages/{vi,en}.json` must stay key-for-key identical. Both are CRLF with
  two-space indent, and reserialising them with a JSON writer rewrites every line -- splice
  additions in as text instead.
- Migrations are append-only and never reprice settled traffic. Adding a model means a new
  row guarded by `WHERE NOT EXISTS`, not closing and reopening an existing price period.