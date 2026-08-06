#!/usr/bin/env python3
"""Check that every model in the catalogue can actually serve a request.

Migration 0020 priced nine models that had been settling at the '*' rate, on the
strength of a few probes each. Pricing a model is a statement that NapKey will serve
it, and two ids in the same catalogue turned out not to answer at all
(claude-sonnet-4.8, gpt-image-2), which is the failure this exists to catch early.

Concurrency is the point, not a speed optimisation. A model that answers one probe can
still fail when several requests land together -- a backend behind the pool may hold a
single slot, or rate-limit per model -- and single-shot probing cannot see that.

Reports per model: how many requests succeeded, the error that killed the rest, and
the latency spread. A model that is slow but correct and one that fails half the time
need different decisions, so they are not collapsed into one number.
"""
import argparse
import json
import os
import ssl
import statistics
import sys
import time
import urllib.error
import urllib.request
from collections import defaultdict
from concurrent.futures import ThreadPoolExecutor

# Cloudflare fronts the gateway and rejects urllib's default agent with error 1010.
HEADERS_BASE = {"Content-Type": "application/json", "Accept": "*/*",
                "User-Agent": "curl/8.5.0"}

# Short and unambiguous: this measures whether a model answers, not how well it writes.
PROMPT = "Reply with exactly one word: ready"


def one_request(base_url, api_key, prefix, model, timeout):
    """Returns (ok, latency_seconds, detail)."""
    url = base_url.rstrip("/") + "/chat/completions"
    payload = json.dumps({
        "model": prefix + model,
        "messages": [{"role": "user", "content": PROMPT}],
        "max_tokens": 32,
        "stream": True,
    }).encode()
    headers = dict(HEADERS_BASE)
    headers["Authorization"] = "Bearer " + api_key
    req = urllib.request.Request(url, data=payload, headers=headers, method="POST")

    started = time.monotonic()
    try:
        with urllib.request.urlopen(req, timeout=timeout,
                                    context=ssl.create_default_context()) as resp:
            text = resp.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as exc:
        return False, time.monotonic() - started, "http_%d" % exc.code
    except Exception as exc:
        return False, time.monotonic() - started, type(exc).__name__

    # The endpoint always answers as an event stream and reports usage on the final
    # chunk. A 200 with no usage is a failure: the request was billed as served but
    # produced nothing the control plane can price.
    usage, content = None, ""
    for line in text.splitlines():
        line = line.strip()
        if not line.startswith("data: ") or line == "data: [DONE]":
            continue
        try:
            chunk = json.loads(line[6:])
        except ValueError:
            continue
        if chunk.get("usage"):
            usage = chunk["usage"]
        for choice in chunk.get("choices") or []:
            content += (choice.get("delta") or {}).get("content") or ""

    elapsed = time.monotonic() - started
    if usage is None:
        return False, elapsed, "no_usage"
    if not content.strip():
        return False, elapsed, "empty_response"
    return True, elapsed, "ok"


def main():
    ap = argparse.ArgumentParser(description="Verify every catalogue model serves traffic.")
    ap.add_argument("--api-key", default=os.environ.get("NINEROUTER_API_KEY"))
    ap.add_argument("--base-url", default=os.environ.get(
        "NINEROUTER_RUNTIME_BASE_URL", "https://gateway-admin.viberouter.io.vn/v1"))
    ap.add_argument("--prefix", default=os.environ.get("NINEROUTER_MODEL_PREFIX", "Viberouter/"))
    ap.add_argument("--models", nargs="*", default=None,
                    help="Defaults to every model the pool publishes.")
    ap.add_argument("--requests", type=int, default=6, help="Requests per model.")
    ap.add_argument("--concurrency", type=int, default=3,
                    help="In flight at once. The key is shared with production; keep it modest.")
    ap.add_argument("--timeout", type=int, default=120)
    ap.add_argument("--json-out")
    args = ap.parse_args()

    if not args.api_key:
        sys.exit("api key required (--api-key or NINEROUTER_API_KEY)")

    models = args.models
    if not models:
        req = urllib.request.Request(args.base_url.rstrip("/") + "/models",
                                     headers=dict(HEADERS_BASE,
                                                  **{"Authorization": "Bearer " + args.api_key}))
        with urllib.request.urlopen(req, timeout=60,
                                    context=ssl.create_default_context()) as resp:
            data = json.loads(resp.read().decode())
        models = sorted({
            m["id"][len(args.prefix):] for m in data.get("data", [])
            if m.get("id", "").startswith(args.prefix)
            and "/" not in m["id"][len(args.prefix):]
            and m["id"][len(args.prefix):] not in ("", "auto")
        })

    tasks = [m for m in models for _ in range(args.requests)]
    print("Checking %d models, %d requests each, %d at a time"
          % (len(models), args.requests, args.concurrency), flush=True)

    results = defaultdict(list)
    with ThreadPoolExecutor(max_workers=args.concurrency) as pool:
        for model, outcome in zip(tasks, pool.map(
                lambda m: one_request(args.base_url, args.api_key, args.prefix, m, args.timeout),
                tasks)):
            results[model].append(outcome)

    print("")
    print("%-24s %8s %9s %9s  %s" % ("model", "ok", "p50", "max", "failures"))
    report = []
    for model in models:
        rows = results[model]
        good = [r for r in rows if r[0]]
        latencies = sorted(r[1] for r in good)
        failures = defaultdict(int)
        for row in rows:
            if not row[0]:
                failures[row[2]] += 1
        p50 = statistics.median(latencies) if latencies else 0.0
        report.append({"model": model, "ok": len(good), "total": len(rows),
                       "p50_s": round(p50, 1),
                       "max_s": round(latencies[-1], 1) if latencies else None,
                       "failures": dict(failures)})
        print("%-24s %8s %8.1fs %8.1fs  %s" % (
            model, "%d/%d" % (len(good), len(rows)), p50,
            latencies[-1] if latencies else 0.0,
            ", ".join("%s x%d" % kv for kv in failures.items()) or "-"))

    broken = [r for r in report if r["ok"] == 0]
    flaky = [r for r in report if 0 < r["ok"] < r["total"]]
    print("")
    if broken:
        print("NOT SERVING -- priced but cannot answer; drop from the catalogue:")
        for r in broken:
            print("   %s (%s)" % (r["model"], ", ".join(r["failures"]) or "unknown"))
    if flaky:
        print("INTERMITTENT -- answers sometimes; a customer will hit the failures:")
        for r in flaky:
            print("   %s: %d of %d succeeded (%s)"
                  % (r["model"], r["ok"], r["total"], ", ".join(r["failures"])))
    if not broken and not flaky:
        print("Every model answered every request.")

    if args.json_out:
        with open(args.json_out, "w", encoding="utf-8") as handle:
            json.dump({"checked_at": time.strftime("%Y-%m-%dT%H:%M:%S%z"),
                       "base_url": args.base_url, "requests_per_model": args.requests,
                       "concurrency": args.concurrency, "results": report},
                      handle, indent=2)
        print("\nWrote %s" % args.json_out)

    return 1 if broken else 0


if __name__ == "__main__":
    sys.exit(main())