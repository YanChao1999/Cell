package metrics

import "net/http"

// UIHandler serves a tiny same-origin dashboard that polls GET /metrics.
// No Grafana/Docker Hub required — open http://control/metrics/ui
func UIHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(metricsUIHTML))
	})
}

const metricsUIHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width, initial-scale=1"/>
<title>Cell metrics</title>
<style>
  :root { --ink:#0e1412; --mute:#5a6b62; --ok:#1f6b4a; --err:#a33; --bg:#e8f0ea; --card:#fff; }
  * { box-sizing: border-box; }
  body { margin:0; font: 15px/1.45 system-ui, sans-serif; color: var(--ink); background: var(--bg); }
  main { max-width: 960px; margin: 0 auto; padding: 1.25rem 1.25rem 3rem; }
  h1 { font-size: 1.35rem; margin: 0 0 .25rem; }
  .sub { color: var(--mute); margin-bottom: 1.25rem; font-size: .92rem; }
  .grid { display: grid; gap: .85rem; grid-template-columns: repeat(auto-fit, minmax(140px, 1fr)); }
  .card { background: var(--card); border-radius: .45rem; padding: .9rem 1rem; border: 1px solid rgba(14,20,18,.08); }
  .card .k { color: var(--mute); font-size: .78rem; text-transform: uppercase; letter-spacing: .04em; }
  .card .v { font-size: 1.6rem; font-weight: 650; margin-top: .2rem; font-variant-numeric: tabular-nums; }
  .card .v.err { color: var(--err); }
  .card .v.ok { color: var(--ok); }
  pre { background: #0e1412; color: #c9d6ce; padding: 1rem; border-radius: .45rem; overflow: auto; font-size: .72rem; max-height: 280px; }
  .row { display: flex; gap: 1rem; flex-wrap: wrap; align-items: baseline; margin: 1.25rem 0 .5rem; }
  .row h2 { font-size: 1rem; margin: 0; }
  .live { color: var(--mute); font-size: .85rem; }
</style>
</head>
<body>
<main>
  <h1>Cell metrics</h1>
  <p class="sub">Live view of <code>/metrics</code> · refreshes every 2s · no Grafana required</p>
  <div class="grid" id="cards"></div>
  <div class="row"><h2>Raw exposition</h2><span class="live" id="ts"></span></div>
  <pre id="raw">loading…</pre>
</main>
<script>
const keys = [
  ["create ok", /cell_create_total\{result="ok"\} (\d+)/, "ok"],
  ["create err", /cell_create_total\{result="error"\} (\d+)/, "err"],
  ["exec ok", /cell_exec_total\{result="ok"\} (\d+)/, "ok"],
  ["exec err", /cell_exec_total\{result="error"\} (\d+)/, "err"],
  ["destroy ok", /cell_destroy_total\{result="ok"\} (\d+)/, "ok"],
  ["destroy err", /cell_destroy_total\{result="error"\} (\d+)/, "err"],
];
function avg(sumRe, countRe, text) {
  const s = text.match(sumRe), c = text.match(countRe);
  if (!s || !c || Number(c[1]) === 0) return "—";
  return (Number(s[1]) / Number(c[1])).toFixed(3) + "s";
}
async function tick() {
  try {
    const res = await fetch("/metrics", { cache: "no-store" });
    const text = await res.text();
    document.getElementById("raw").textContent = text;
    document.getElementById("ts").textContent = "updated " + new Date().toLocaleTimeString();
    const cards = keys.map(([label, re, cls]) => {
      const m = text.match(re);
      const v = m ? m[1] : "0";
      return '<div class="card"><div class="k">' + label + '</div><div class="v ' + cls + '">' + v + '</div></div>';
    });
    cards.push('<div class="card"><div class="k">avg create</div><div class="v">' +
      avg(/cell_create_duration_seconds_sum ([0-9.]+)/, /cell_create_duration_seconds_count (\d+)/, text) + '</div></div>');
    cards.push('<div class="card"><div class="k">avg exec</div><div class="v">' +
      avg(/cell_exec_duration_seconds_sum ([0-9.]+)/, /cell_exec_duration_seconds_count (\d+)/, text) + '</div></div>');
    document.getElementById("cards").innerHTML = cards.join("");
  } catch (e) {
    document.getElementById("ts").textContent = "error: " + e;
  }
}
tick();
setInterval(tick, 2000);
</script>
</body>
</html>
`