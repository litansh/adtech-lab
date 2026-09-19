import json, urllib.request, collections, time
SSP="http://127.0.0.1:8090"; DSP="http://127.0.0.1:8081"
def post(url, body):
    r=urllib.request.Request(url, data=json.dumps(body).encode(),
        headers={"Content-Type":"application/json",
                 "User-Agent":"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0) AppleWebKit/605.1.15 Safari/604.1"})
    with urllib.request.urlopen(r, timeout=5) as resp: return json.load(resp)
def get(url):
    with urllib.request.urlopen(url, timeout=5) as resp: return json.load(resp)

N=300
won=collections.Counter(); status=collections.Counter(); lat=[]
sspWins=0; sspSpend=0.0
for i in range(N):
    body={"placement_id":"game_sidebar","game":"xo","session_id":f"s-{i}",
          "device_type":"mobile","w":300,"h":250,"env":"production"}
    d=post(SSP+"/ad/request", body)
    a=d.get("auction") or {}
    for b in a.get("bids",[]):
        if b.get("buyer_id")=="buyer-mini-dsp":
            status[b.get("status")]+=1
            if b.get("latency_ms"): lat.append(b["latency_ms"])
    w=a.get("winner") or {}
    if w.get("buyer_id"): won[w["buyer_id"]]+=1
    if w.get("buyer_id")=="buyer-mini-dsp":
        sspWins+=1; sspSpend += (a.get("clearing_price") or 0)/1000

print(f"SSP view — {N} requests")
print("  mini-dsp responses:", dict(status))
if lat: print(f"  mini-dsp latency: min={min(lat)}ms med={sorted(lat)[len(lat)//2]}ms max={max(lat)}ms")
print("  auction winners:", dict(won))
print(f"  SSP believes mini-dsp WON {sspWins} times, spending ${sspSpend:.4f}")

time.sleep(1.5)
st=get(DSP+"/stats")
c=[x for x in st["campaigns"] if x["campaign_id"]=="cmp-acme-retarget"][0]
print(f"\nDSP view — the BUYER's own numbers")
print(f"  bids submitted:  {c['bids_submitted']}")
print(f"  wins notified:   {c['wins_notified']}")
print(f"  spend:           ${c['spend_usd']:.4f}")

print(f"\nDISCREPANCY")
print(f"  wins:  SSP {sspWins}  vs  DSP {c['wins_notified']}   "
      f"({(sspWins-c['wins_notified'])/max(sspWins,1)*100:.1f}% under-counted by the buyer)")
print(f"  spend: SSP ${sspSpend:.4f}  vs  DSP ${c['spend_usd']:.4f}")
