#!/usr/bin/env python3
"""全网 /24 普查：找出对你这条线路真正快的 Cloudflare 前缀。

背景：Cloudflare 的 anycast 落点由前缀（/24）决定，同一个 /24 里换地址落点
不变，跨一个 /24 就可能从东京变成洛杉矶。随机采样 300 个地址，命中好前缀的
概率极低，所以「最优」会长期卡在同一个机房。这个脚本把官方 15 段里的每个
/24 都探一遍，产出一份可以直接贴进「设置 → 候选前缀」的清单。

只读：只做 TCP 443 握手和 /cdn-cgi/trace，不写文件、不改 DNS。

用法
    python3 prefix-survey.py                  # 人类可读摘要 + JSON
    python3 prefix-survey.py --shortlist      # 只输出前缀清单，可直接粘贴
    python3 prefix-survey.py --max-rtt 120    # 自定义清单阈值（默认 150ms）
    python3 prefix-survey.py --workers 32     # 降低并发（小机器/家宽）

建议在每条线路各自的探针上跑一遍：电信的清单给联通用，轻则慢 4 倍，
重则慢 70 倍——三网的好前缀交集实测为 0。
"""

import ipaddress
import json
import socket
import ssl
import statistics
import sys
import time
from concurrent.futures import ThreadPoolExecutor

RANGES = [
    "173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22",
    "141.101.64.0/18", "108.162.192.0/18", "190.93.240.0/20", "188.114.96.0/20",
    "197.234.240.0/22", "198.41.128.0/17", "162.158.0.0/15", "104.16.0.0/13",
    "104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22",
]

TRACE = b"GET /cdn-cgi/trace HTTP/1.1\r\nHost: cloudflare.com\r\nConnection: close\r\n\r\n"


def parse_args(argv):
    opts = {"shortlist": False, "max_rtt": 150.0, "workers": 64, "limit": 0}
    i = 0
    while i < len(argv):
        a = argv[i]
        if a == "--shortlist":
            opts["shortlist"] = True
        elif a == "--max-rtt" and i + 1 < len(argv):
            opts["max_rtt"] = float(argv[i + 1])
            i += 1
        elif a == "--workers" and i + 1 < len(argv):
            opts["workers"] = int(argv[i + 1])
            i += 1
        elif a == "--limit" and i + 1 < len(argv):
            opts["limit"] = int(argv[i + 1])
            i += 1
        elif a in ("-h", "--help"):
            print(__doc__)
            raise SystemExit(0)
        i += 1
    return opts


def build_targets(limit):
    targets = []
    for r in RANGES:
        n = ipaddress.ip_network(r)
        base = int(n.network_address)
        for i in range(n.num_addresses // 256):
            # 避开网络号/广播号；每个 /24 只取一个取样点
            targets.append(str(ipaddress.ip_address(base + i * 256 + 66)))
    return targets[:limit] if limit > 0 else targets


ctx = ssl.create_default_context()
ctx.check_hostname = False
ctx.verify_mode = ssl.CERT_NONE


def probe(ip):
    t = time.perf_counter()
    raw = None
    try:
        raw = socket.create_connection((ip, 443), timeout=1.2)
        rtt = (time.perf_counter() - t) * 1000
        raw.settimeout(1.8)
        with ctx.wrap_socket(raw, server_hostname="cloudflare.com") as s:
            raw = None  # 交给 wrap_socket 管理
            s.sendall(TRACE)
            buf = b""
            while True:
                chunk = s.recv(4096)
                if not chunk:
                    break
                buf += chunk
        body = buf.decode("utf-8", "replace")
        d = dict(line.split("=", 1) for line in body.splitlines() if line.count("=") == 1)
        return {"ip": ip, "ok": True, "rtt": round(rtt, 1), "colo": d.get("colo", "?")}
    except Exception as e:
        return {"ip": ip, "ok": False, "err": type(e).__name__}
    finally:
        if raw is not None:
            try:
                raw.close()
            except Exception:
                pass


def prefix_of(ip):
    return ".".join(ip.split(".")[:3]) + ".0/24"


def main():
    opts = parse_args(sys.argv[1:])
    targets = build_targets(opts["limit"])

    started = time.perf_counter()
    results = []
    with ThreadPoolExecutor(max_workers=opts["workers"]) as pool:
        for done, r in enumerate(pool.map(probe, targets), 1):
            results.append(r)
            if not opts["shortlist"] and done % 1000 == 0:
                print(f"PROGRESS {done}/{len(targets)}", file=sys.stderr, flush=True)

    ok = [r for r in results if r["ok"]]

    if opts["shortlist"]:
        # 每个前缀取最快的一次，按延迟排序，只留阈值内的
        best = {}
        for r in ok:
            if r["rtt"] > opts["max_rtt"]:
                continue
            p = prefix_of(r["ip"])
            if p not in best or r["rtt"] < best[p]:
                best[p] = r["rtt"]
        if not best:
            print(f"# 没有找到 <{opts['max_rtt']:.0f}ms 的前缀，这条线路可能本身就没有近的落点", file=sys.stderr)
            return
        print(f"# 共 {len(results)} 个 /24，{len(ok)} 个连通，"
              f"{len(best)} 个 <{opts['max_rtt']:.0f}ms（单次测量，建议在面板里复测后再定）")
        for p, rtt in sorted(best.items(), key=lambda kv: kv[1]):
            print(p)
        return

    elapsed = time.perf_counter() - started
    print(f"SURVEY total={len(results)} ok={len(ok)} fail={len(results) - len(ok)} elapsed={elapsed:.0f}s")

    by_colo = {}
    for r in ok:
        by_colo.setdefault(r["colo"], []).append(r["rtt"])
    print("\n落点分布（按数量）：")
    for colo, vals in sorted(by_colo.items(), key=lambda kv: -len(kv[1])):
        print(f"  {colo:8s} n={len(vals):4d}  中位 {statistics.median(vals):6.0f}ms  最快 {min(vals):6.0f}ms")

    print("\nRTT 最快的 40 个前缀：")
    for r in sorted(ok, key=lambda x: x["rtt"])[:40]:
        print(f"  {r['ip']:16s} {r['rtt']:7.1f}ms  {r['colo']}")

    print("\n@@JSON@@" + json.dumps(results, separators=(",", ":")))


if __name__ == "__main__":
    main()
