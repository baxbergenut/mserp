"""Export sanitized HTTP performance records for the currently running release.
No credentials, request bodies, URLs with query strings, or user identities are saved.
"""
import argparse, csv, datetime, json, math, re, subprocess
from pathlib import Path

def ssh(host, *args):
    return subprocess.check_output(["ssh", "-o", "BatchMode=yes", host, *args], text=True, encoding="utf-8")

def main():
    p=argparse.ArgumentParser()
    p.add_argument("--host", required=True)
    p.add_argument("--output", type=Path, required=True)
    a=p.parse_args()
    release=ssh(a.host,"readlink", "-f", "/opt/mserp/current").strip().split("/")[-1]
    props=dict(line.split("=",1) for line in ssh(a.host,"systemctl", "show", "mserp-api", "-p", "MainPID", "-p", "ExecMainStartTimestamp").splitlines())
    until=datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%d %H:%M:%S UTC")
    raw=ssh(a.host,"journalctl", "-u", "mserp-api", "_PID="+props["MainPID"], "--since", "'"+props["ExecMainStartTimestamp"]+"'", "--until", "'"+until+"'", "-o", "json", "--no-pager")
    samples=[]
    for line in raw.splitlines():
        item=json.loads(line);msg=item.get("MESSAGE","")
        if 'http performance' not in msg: continue
        fields=dict(re.findall(r'(\w+)=([^ ]+)',msg))
        route=fields.get("route","").strip('"')
        if not route.startswith("/") or "?" in route or re.search(r'accounting|payroll|driver-pay|investor-pay|driver-charges|truck-charges|escrow|/events$',route):continue
        if fields.get("method")!="GET":continue
        def number(key):
            try:return float(fields[key].strip('"'))
            except (ValueError,KeyError):return None
        samples.append(dict(at=item.get("__REALTIME_TIMESTAMP"),route=route,scenario=fields.get("scenario","default").strip('"'),status=number("status"),duration_ms=number("duration_ms"),db_calls=number("db_calls"),db_ms=number("db_ms"),pool_ms=number("pool_ms"),response_bytes=number("response_bytes")))
    summary=[]
    for route,scenario in sorted({(s["route"],s["scenario"]) for s in samples}):
        rows=[s for s in samples if s["route"]==route and s["scenario"]==scenario];timings=sorted(s["duration_ms"] for s in rows if s["duration_ms"] is not None)
        if not timings:continue
        quantile=lambda q:timings[max(0,math.ceil(q*len(timings))-1)]
        summary.append(dict(route=route,scenario=scenario,samples=len(rows),failures=sum(s["status"] is None or s["status"]>=400 for s in rows),p50_ms=quantile(.5),p95_ms=quantile(.95),p99_ms=quantile(.99),mean_db_calls=sum(s["db_calls"] or 0 for s in rows)/len(rows),mean_db_ms=sum(s["db_ms"] or 0 for s in rows)/len(rows)))
    # Refuse to label a mixed/deployed process as a single-release measurement.
    current=ssh(a.host,"systemctl", "show", "mserp-api", "-p", "MainPID", "--value").strip()
    if current!=props["MainPID"]:raise SystemExit("Release changed while reading logs; retry with the new process.")
    a.output.parent.mkdir(parents=True,exist_ok=True)
    a.output.with_suffix(".json").write_text(json.dumps(dict(release=release,pid=props["MainPID"],since=props["ExecMainStartTimestamp"],until=until,metric="Observed API duration; not browser page-load p99. Small samples are not reliable tail estimates. Query variants share route templates.",summary=summary,samples=samples),indent=2),encoding="utf-8")
    with a.output.with_suffix(".csv").open("w",newline="",encoding="utf-8") as f:
        if summary:w=csv.DictWriter(f,fieldnames=list(summary[0]));w.writeheader();w.writerows(summary)
    print(json.dumps(dict(release=release,samples=len(samples),routes=len(summary),output=str(a.output)),indent=2))
if __name__=="__main__":main()
