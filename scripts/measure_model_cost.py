#!/usr/bin/env python3
"""Measure the real upstream cost of each model NapKey sells.

The price book carries one cost basis -- 2,097 VND/1M tokens plus 110 VND/call
(migrations 0018 and 0019) -- and docs/OPERATIONS.md records that it was measured on
Claude traffic only. Every other model falls through to the '*' row at that same
basis, unverified. This measures what the upstream actually bills, per model.

The upstream bills per token and prepends its own prompt before counting, so:

    billed(n) = overhead + rate * n          n = paragraph count

Solving both from three sizes means no client-side tokenizer is needed, which
matters because a tokenizer estimate would land entirely inside the overhead figure.

The pool routes one model to several backends whose injected prompts differ in size,
so each size is probed --repeat times and the mode is taken. mode_share reports how
dominant the winning backend was; a low value means cost genuinely varies per request.

Linux/server counterpart of scripts/measure-model-cost.ps1. Standard library only.
"""
import argparse
import json
import os
import random
import ssl
import sys
import time
import urllib.error
import urllib.request
from collections import defaultdict
from concurrent.futures import ThreadPoolExecutor

# Price book basis, from migrations 0018 and 0019. Read-only here.
RETAIL_PER_MILLION = 12000
UPSTREAM_PER_MILLION = 2097
RETAIL_FEE_VND = 300
UPSTREAM_FEE_VND = 110

SIZES_IN_PARAGRAPHS = (4, 12, 28)

# Natural prose: repeated filler such as "lorem lorem" compresses far better than
# anything a customer sends, which would put the measured slope below the real rate.
PARAGRAPH = (
    "The quarterly report shows steady growth across every regional market, with "
    "margins holding near the level management guided to at the start of the year. "
    "Operating expenses rose modestly, driven mostly by hiring in support and "
    "engineering. "
)

OUTPUT_PROMPT = ("Explain what an API gateway does and why a team would put one in "
                 "front of their services.")
OUTPUT_BUDGET = 600

# Roughly one coding-agent step, so models are compared on the same workload.
REFERENCE_CALLER_TOKENS = 1000


def post_json(url, payload, headers, timeout):
    body = json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(url, data=body, headers=headers, method="POST")
    ctx = ssl.create_default_context()
    with urllib.request.urlopen(req, timeout=timeout, context=ctx) as resp:
        return resp.read().decode("utf-8", "replace")


def probe(args, upstream_model, content, max_tokens):
    """One billed request. Returns (prompt_tokens, completion_tokens, error)."""
    url = args.base_url.rstrip("/") + "/chat/completions"
    # Cloudflare fronts the gateway and rejects urllib's default agent with error
    # 1010 before the request reaches the upstream at all.
    headers = {"Authorization": "Bearer " + args.api_key,
               "Content-Type": "application/json",
               "Accept": "*/*",
               "User-Agent": "curl/8.5.0"}
    payload = {"model": upstream_model,
               "messages": [{"role": "user", "content": content}],
               "max_tokens": max_tokens,
               "stream": True}

    for attempt in range(1, args.max_attempts + 1):
        try:
            text = post_json(url, payload, headers, args.timeout)
            # The endpoint always answers as text/event-stream, whatever "stream" is
            # set to, and reports usage only on the final chunk. Parsing the body as
            # one JSON object fails outright, so every data: line is scanned and the
            # last usage seen wins.
            usage = {}
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
            if not usage:
                return None, None, "no_usage_in_stream"
            return int(usage.get("prompt_tokens", 0)), int(usage.get("completion_tokens", 0)), None
        except urllib.error.HTTPError as exc:
            status = exc.code
            err = "http_%d" % status
        except Exception:
            status = 0
            err = "request_failed"

        # 429 and 5xx are the upstream being busy. Any other 4xx is a verdict on the
        # request itself, so retrying only spends real money on the same refusal.
        retryable = status == 429 or status >= 500 or status == 0
        if not retryable or attempt == args.max_attempts:
            return None, None, err
        time.sleep(min(60, 2 ** attempt) + random.uniform(0, 2))
    return None, None, "exhausted"


def get_pool_models(args):
    url = args.base_url.rstrip("/") + "/models"
    req = urllib.request.Request(url, headers={"Authorization": "Bearer " + args.api_key,
                                               "Accept": "*/*", "User-Agent": "curl/8.5.0"})
    with urllib.request.urlopen(req, timeout=60, context=ssl.create_default_context()) as resp:
        data = json.loads(resp.read().decode("utf-8"))
    out = set()
    for entry in data.get("data", []):
        ident = str(entry.get("id", ""))
        if not ident.lower().startswith(args.prefix.lower()):
            continue
        public = ident[len(args.prefix):].strip()
        # A nested namespace has no public id to map back to; "auto" picks a
        # different model per request, so its cost describes whatever it routed to.
        if not public or "/" in public or public == "auto":
            continue
        if not args.include_thinking and public.endswith("-thinking"):
            continue
        out.add(public)
    return sorted(out)


def mode(values, tolerance=32):
    """Most frequent value, with the share that agreed. Mode not mean, because
    averaging two backends reports a number neither of them charges."""
    if not values:
        return None, None
    best, best_count = None, 0
    for candidate in values:
        count = sum(1 for v in values if abs(v - candidate) <= tolerance)
        if count > best_count:
            best, best_count = candidate, count
    return best, round(best_count / float(len(values)), 2)


def main():
    ap = argparse.ArgumentParser(description="Measure per-model upstream cost.")
    ap.add_argument("--api-key", default=os.environ.get("NINEROUTER_API_KEY"))
    ap.add_argument("--base-url", default=os.environ.get(
        "NINEROUTER_RUNTIME_BASE_URL", "https://gateway-admin.viberouter.io.vn/v1"))
    ap.add_argument("--prefix", default=os.environ.get("NINEROUTER_MODEL_PREFIX", "Viberouter/"))
    ap.add_argument("--models", nargs="*", default=None)
    ap.add_argument("--repeat", type=int, default=3, help="Probes per size; below 3 the mode is meaningless.")
    ap.add_argument("--concurrency", type=int, default=4, help="Keep modest: the key is shared with production.")
    ap.add_argument("--timeout", type=int, default=300)
    ap.add_argument("--max-attempts", type=int, default=5)
    ap.add_argument("--json-out")
    ap.add_argument("--include-thinking", action="store_true")
    ap.add_argument("--skip-output-probe", action="store_true")
    args = ap.parse_args()

    if not args.api_key:
        sys.exit("api key required (--api-key or NINEROUTER_API_KEY)")
    if not 1 <= args.concurrency <= 16:
        sys.exit("concurrency must be between 1 and 16")

    models = args.models
    if not models:
        print("Reading the pool catalog...", flush=True)
        models = get_pool_models(args)

    tasks = []
    for model in models:
        for size in SIZES_IN_PARAGRAPHS:
            content = "Summarize this. " + PARAGRAPH * size
            for _ in range(args.repeat):
                tasks.append((model, "size", size, content, 16))
        if not args.skip_output_probe:
            tasks.append((model, "output", 0, OUTPUT_PROMPT, OUTPUT_BUDGET))

    print("Measuring %d models with %d probes, %d at a time, against %s"
          % (len(models), len(tasks), args.concurrency, args.base_url), flush=True)

    buckets = defaultdict(lambda: {"sizes": defaultdict(list), "output": [], "errors": []})
    done = 0

    def run(task):
        model, kind, size, content, max_tokens = task
        prompt_tokens, completion_tokens, err = probe(args, args.prefix + model, content, max_tokens)
        return model, kind, size, prompt_tokens, completion_tokens, err

    with ThreadPoolExecutor(max_workers=args.concurrency) as pool:
        for model, kind, size, prompt_tokens, completion_tokens, err in pool.map(run, tasks):
            done += 1
            print("  %d/%d %-28s %-6s %s" % (
                done, len(tasks), model, kind,
                err if err else ("prompt=%d" % prompt_tokens if kind == "size"
                                 else "output=%d" % completion_tokens)), flush=True)
            if err:
                buckets[model]["errors"].append(err)
            elif kind == "size":
                buckets[model]["sizes"][size].append(prompt_tokens)
            else:
                buckets[model]["output"].append(completion_tokens)

    results = []
    for model in models:
        bucket = buckets.get(model)
        measured = [s for s in SIZES_IN_PARAGRAPHS if bucket and bucket["sizes"].get(s)]
        if len(measured) < 2:
            errors = bucket["errors"] if bucket else []
            status = max(set(errors), key=errors.count) if errors else "no_data"
            results.append({"model": model, "status": status})
            continue

        billed, shares = {}, []
        for size in measured:
            value, share = mode(bucket["sizes"][size])
            billed[size] = value
            shares.append(share)

        first, last = measured[0], measured[-1]
        rate = (billed[last] - billed[first]) / float(last - first)
        overhead = int(round(billed[first] - rate * first))

        # A model that is not linear in caller text is not billed the way this basis
        # assumes, so it is flagged rather than averaged over.
        linearity = None
        if len(measured) >= 3:
            mid = measured[1]
            predicted = overhead + rate * mid
            if billed[mid] > 0:
                linearity = round(abs(billed[mid] - predicted) / float(billed[mid]), 3)

        output_tokens = int(sum(bucket["output"]) / len(bucket["output"])) if bucket["output"] else 0
        total = overhead + REFERENCE_CALLER_TOKENS + output_tokens
        upstream_vnd = round(total * UPSTREAM_PER_MILLION / 1e6 + UPSTREAM_FEE_VND, 2)
        retail_vnd = round(total * RETAIL_PER_MILLION / 1e6 + RETAIL_FEE_VND, 2)

        results.append({
            "model": model,
            "overhead_tokens": overhead,
            "tok_per_para": round(rate, 1),
            "linearity": linearity,
            "mode_share": round(sum(shares) / len(shares), 2),
            "output_tokens": output_tokens,
            "billed_tokens": total,
            "upstream_vnd": upstream_vnd,
            "retail_vnd": retail_vnd,
            "margin_pct": round(100.0 * (retail_vnd - upstream_vnd) / retail_vnd, 1),
            "status": "partial" if bucket["errors"] else "ok",
        })

    # Measured rows first, cheapest-first within them, so the table opens on the
    # comparison it exists for.
    results.sort(key=lambda r: (r.get("upstream_vnd") is None, r.get("upstream_vnd") or 0))

    header = ("model", "overhead", "tok/para", "linear", "modeshr", "output",
              "billed", "upstream", "retail", "margin%", "status")
    fmt = "%-28s %9s %9s %7s %8s %7s %8s %9s %9s %8s %s"
    print("")
    print(fmt % header)
    for r in results:
        print(fmt % (r["model"],
                     r.get("overhead_tokens", "-"), r.get("tok_per_para", "-"),
                     r.get("linearity", "-"), r.get("mode_share", "-"),
                     r.get("output_tokens", "-"), r.get("billed_tokens", "-"),
                     r.get("upstream_vnd", "-"), r.get("retail_vnd", "-"),
                     r.get("margin_pct", "-"), r["status"]))

    ok = [r for r in results if r["status"] in ("ok", "partial")]
    if ok:
        low, high = ok[0], ok[-1]
        print("\nSummary")
        print("  cheapest request : %s at %s VND (%s overhead tokens)"
              % (low["model"], low["upstream_vnd"], low["overhead_tokens"]))
        print("  dearest request  : %s at %s VND (%s overhead tokens)"
              % (high["model"], high["upstream_vnd"], high["overhead_tokens"]))
        print("  margin range     : %s%% to %s%%"
              % (min(r["margin_pct"] for r in ok), max(r["margin_pct"] for r in ok)))
        print("  overhead range   : %s to %s tokens per request"
              % (min(r["overhead_tokens"] for r in ok), max(r["overhead_tokens"] for r in ok)))

        unstable = [r for r in ok if r["mode_share"] < 0.6]
        if unstable:
            print("\n  Cost varies per request. The pool answered these from backends with")
            print("  different injected prompts, so the figure is an average, not a rate:")
            for r in unstable:
                print("    %s: mode agreed on %d%% of probes" % (r["model"], r["mode_share"] * 100))

        nonlinear = [r for r in ok if r["linearity"] is not None and r["linearity"] > 0.1]
        if nonlinear:
            print("\n  Not linear in caller tokens, so a per-token basis does not describe these:")
            for r in nonlinear:
                print("    %s: mid-point off by %d%%" % (r["model"], r["linearity"] * 100))

    failed = [r for r in results if r["status"] not in ("ok", "partial")]
    if failed:
        print("\n  %d model(s) not measured: %s" % (
            len(failed), ", ".join("%s (%s)" % (r["model"], r["status"]) for r in failed)))

    if args.json_out:
        payload = {
            "measured_at": time.strftime("%Y-%m-%dT%H:%M:%S%z"),
            "base_url": args.base_url,
            "prefix": args.prefix,
            "repeat": args.repeat,
            "sizes_in_paragraphs": list(SIZES_IN_PARAGRAPHS),
            "reference_caller_tokens": REFERENCE_CALLER_TOKENS,
            "basis": {"retail_per_million": RETAIL_PER_MILLION,
                      "upstream_per_million": UPSTREAM_PER_MILLION,
                      "retail_fee_vnd": RETAIL_FEE_VND,
                      "upstream_fee_vnd": UPSTREAM_FEE_VND},
            "results": results,
        }
        with open(args.json_out, "w", encoding="utf-8") as handle:
            json.dump(payload, handle, indent=2, ensure_ascii=False)
        print("\nWrote %s" % args.json_out)


if __name__ == "__main__":
    main()
