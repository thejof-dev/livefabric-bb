package handlers

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// ifaceCounters holds the cumulative byte/packet counters for one interface as
// reported by the kernel in /proc/net/dev. Under `--network host` these are the
// HOST interface counters, which is exactly what we want for wire-rate visibility.
type ifaceCounters struct {
	Name      string `json:"name"`
	RxBytes   uint64 `json:"rxBytes"`
	RxPackets uint64 `json:"rxPackets"`
	TxBytes   uint64 `json:"txBytes"`
	TxPackets uint64 `json:"txPackets"`
}

// readProcNetDev parses /proc/net/dev into per-interface cumulative counters.
// The loopback interface is skipped. On non-Linux hosts (no procfs) it returns
// an empty slice with no error so the endpoint degrades gracefully.
func readProcNetDev() ([]ifaceCounters, error) {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		if os.IsNotExist(err) {
			return []ifaceCounters{}, nil
		}
		return nil, err
	}
	defer f.Close()

	out := make([]ifaceCounters, 0, 8)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue // header rows have no colon
		}
		name := strings.TrimSpace(line[:colon])
		if name == "" || name == "lo" {
			continue
		}
		fields := strings.Fields(line[colon+1:])
		// Layout: rxBytes rxPackets rxErrs rxDrop rxFifo rxFrame rxCompressed
		//         rxMulticast txBytes txPackets ...
		if len(fields) < 10 {
			continue
		}
		parse := func(i int) uint64 { v, _ := strconv.ParseUint(fields[i], 10, 64); return v }
		out = append(out, ifaceCounters{
			Name:      name,
			RxBytes:   parse(0),
			RxPackets: parse(1),
			TxBytes:   parse(8),
			TxPackets: parse(9),
		})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// monitorAPIHandler serves raw cumulative interface counters as JSON. The
// browser samples this endpoint on an interval and computes rates client-side,
// so the server stays stateless.
func monitorAPIHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ifaces, err := readProcNetDev()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"interfaces": ifaces,
	})
}

// monitorPageHandler serves a dependency-free live throughput page.
func monitorPageHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(monitorPageHTML))
}

const monitorPageHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>LiveFabric BB — Network Monitor</title>
<style>
  :root { color-scheme: dark; }
  body { font-family: system-ui, -apple-system, Segoe UI, Roboto, sans-serif;
         background:#161616; color:#f4f4f4; margin:0; padding:2rem; }
  main { max-width:820px; margin:0 auto; }
  h1 { font-size:1.25rem; font-weight:600; margin:0 0 .25rem; }
  p.sub { color:#a8a8a8; margin:0 0 1.5rem; font-size:.85rem; }
  table { width:100%; border-collapse:collapse; font-variant-numeric:tabular-nums; }
  th, td { text-align:right; padding:.5rem .75rem; border-bottom:1px solid #393939; }
  th:first-child, td:first-child { text-align:left; }
  thead th { color:#a8a8a8; font-weight:600; font-size:.8rem; text-transform:uppercase; letter-spacing:.03em; }
  td.name { font-family:ui-monospace, SFMono-Regular, Menlo, monospace; color:#78a9ff; }
  td.hot { color:#ff8389; font-weight:600; }
  .foot { margin-top:1rem; color:#6f6f6f; font-size:.8rem; }
  .foot code { color:#a8a8a8; }
</style>
</head>
<body>
<main>
  <h1>Network Monitor</h1>
  <p class="sub">Live per-interface throughput from <code>/proc/net/dev</code> (host namespace under <code>--network host</code>). Rates are computed from counter deltas, sampled every 1&nbsp;s.</p>
  <table>
    <thead>
      <tr><th>Interface</th><th>RX Mbps</th><th>TX Mbps</th><th>RX total</th><th>TX total</th></tr>
    </thead>
    <tbody id="rows"><tr><td colspan="5">Sampling…</td></tr></tbody>
  </table>
  <p class="foot">BB WHEP egress is typically ~0.1–3&nbsp;Mbps per viewer. Any interface showing sustained tens of Mbps that BB can't account for is coming from another process on the host. Rows &gt; 10&nbsp;Mbps are highlighted.</p>
</main>
<script>
  var prev = null, prevT = 0;
  function human(b){
    var u=['B','KB','MB','GB','TB']; var i=0; var v=b;
    while(v>=1024&&i<u.length-1){v/=1024;i++;}
    return v.toFixed(v<10&&i>0?1:0)+' '+u[i];
  }
  function fmtMbps(x){ return (x).toFixed(x<10?2:1); }
  async function tick(){
    try{
      var t = Date.now();
      var r = await fetch('/api/netstat', {cache:'no-store'});
      var j = await r.json();
      var cur = {};
      (j.interfaces||[]).forEach(function(i){ cur[i.name]=i; });
      var rows='';
      var names = Object.keys(cur).sort();
      names.forEach(function(n){
        var c = cur[n];
        var rxM='—', txM='—', hot='';
        if(prev && prev[n]){
          var dt = (t-prevT)/1000;
          if(dt>0){
            var rx = (c.rxBytes-prev[n].rxBytes)*8/1e6/dt;
            var tx = (c.txBytes-prev[n].txBytes)*8/1e6/dt;
            if(rx<0)rx=0; if(tx<0)tx=0;
            rxM=fmtMbps(rx); txM=fmtMbps(tx);
            if(rx>10||tx>10) hot=' class="hot"';
          }
        }
        rows += '<tr><td class="name">'+n+'</td>'
             +  '<td'+hot+'>'+rxM+'</td>'
             +  '<td'+hot+'>'+txM+'</td>'
             +  '<td>'+human(c.rxBytes)+'</td>'
             +  '<td>'+human(c.txBytes)+'</td></tr>';
      });
      document.getElementById('rows').innerHTML = rows || '<tr><td colspan="5">No interfaces.</td></tr>';
      prev = cur; prevT = t;
    }catch(e){
      document.getElementById('rows').innerHTML = '<tr><td colspan="5">Error: '+e.message+'</td></tr>';
    }
  }
  tick();
  setInterval(tick, 1000);
</script>
</body>
</html>`
