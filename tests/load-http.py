#!/usr/bin/env python3
"""Bounded HTTP smoke load, with keep-alive. Run only against an authorized test host.
Example: python3 tests/load-http.py --url http://127.0.0.1/ --host test.invalid
Authenticated checks can use XPANEL_LOAD_COOKIE; cookies are never printed.
"""
import argparse
import concurrent.futures
import http.client
import json
import math
import os
import ssl
import threading
import time
import urllib.parse


def benchmark(url, concurrency=8, requests=300, host=None, cookie=None, contains=None):
    parsed = urllib.parse.urlsplit(url)
    if parsed.scheme not in ("http", "https"):
        raise ValueError("HTTP(S) URL required")
    local = threading.local()
    connections = []
    lock = threading.Lock()
    headers = {}
    if host:
        headers["Host"] = host
    if cookie:
        headers["Cookie"] = cookie
    target = urllib.parse.urlunsplit(("", "", parsed.path or "/", parsed.query, ""))

    def one(_):
        start = time.perf_counter()
        try:
            if not hasattr(local, "connection"):
                if parsed.scheme == "https":
                    # Test panels commonly use a self-signed certificate.
                    conn = http.client.HTTPSConnection(parsed.hostname, parsed.port, timeout=15,
                                                       context=ssl._create_unverified_context())
                else:
                    conn = http.client.HTTPConnection(parsed.hostname, parsed.port, timeout=15)
                local.connection = conn
                with lock:
                    connections.append(conn)
            local.connection.request("GET", target, headers=headers)
            response = local.connection.getresponse()
            data = response.read()
            ok = response.status == 200 and (contains is None or contains.encode() in data)
        except Exception:
            ok = False
            if hasattr(local, "connection"):
                local.connection.close()
                del local.connection
        return (time.perf_counter() - start) * 1000, ok

    start = time.perf_counter()
    try:
        with concurrent.futures.ThreadPoolExecutor(max_workers=concurrency) as pool:
            results = list(pool.map(one, range(requests)))
    finally:
        for conn in connections:
            conn.close()
    elapsed = time.perf_counter() - start
    latencies = sorted(ms for ms, _ in results)
    percentile = lambda p: round(latencies[min(len(latencies)-1, math.ceil(p*len(latencies))-1)], 2)
    return {"concurrency": concurrency, "requests": requests, "errors": sum(not ok for _, ok in results),
            "seconds": round(elapsed, 3), "rps": round(requests/elapsed, 1),
            "p50_ms": percentile(.5), "p95_ms": percentile(.95), "p99_ms": percentile(.99)}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--url", required=True)
    parser.add_argument("--host")
    parser.add_argument("--requests", type=int, default=300)
    parser.add_argument("--concurrency", type=int, default=8)
    parser.add_argument("--contains")
    args = parser.parse_args()
    if not 1 <= args.requests <= 100000 or not 1 <= args.concurrency <= 128:
        parser.error("requests must be 1..100000; concurrency 1..128")
    result = benchmark(args.url, args.concurrency, args.requests, args.host,
                       os.environ.get("XPANEL_LOAD_COOKIE"), args.contains)
    print(json.dumps(result))
    raise SystemExit(1 if result["errors"] else 0)
