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

## Cloudflare

Create a narrowly scoped WAF skip rule for the Casso source IPs and only the
webhook path. Keep SSL mode at **Full (strict)**. Confirm with Casso support that
their request receives HTTP 200 within five seconds; do not disable the WAF for
the whole API hostname.

## Incident priorities

- Critical: wallet drift, expired open holds, stuck payment processing, usage reports dropped.
- Warning: unmatched Casso payments, key sync failures, pool capacity below one usable account.
- Never auto-correct wallet drift. Investigate and post an audited adjustment if required.
