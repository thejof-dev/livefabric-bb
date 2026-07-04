package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/glimesh/broadcast-box/internal/settings"
)

// settingsAPIHandler serves GET (read) and POST/PUT (update) of the runtime
// settings as JSON.
func settingsAPIHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		settingsWriteJSON(w, http.StatusOK, settings.Get())

	case http.MethodPost, http.MethodPut:
		var in settings.Settings
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in); err != nil {
			settingsWriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
			return
		}

		saved, err := settings.Update(in)
		if err != nil {
			settingsWriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		settingsWriteJSON(w, http.StatusOK, saved)

	default:
		w.Header().Set("Allow", "GET, POST, PUT")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func settingsWriteJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// settingsPageHandler serves a minimal dependency-free admin page.
func settingsPageHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(settingsPageHTML))
}

const settingsPageHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>LiveFabric BB — Settings</title>
<style>
  :root { color-scheme: dark; }
  body { font-family: system-ui, -apple-system, Segoe UI, Roboto, sans-serif;
         background:#161616; color:#f4f4f4; margin:0; padding:2rem; }
  main { max-width:560px; margin:0 auto; }
  h1 { font-size:1.25rem; font-weight:600; margin:0 0 .25rem; }
  p.sub { color:#a8a8a8; margin:0 0 1.5rem; font-size:.9rem; }
  fieldset { border:1px solid #393939; padding:1rem 1.25rem 1.25rem; margin:0 0 1rem; }
  legend { padding:0 .5rem; color:#c6c6c6; font-size:.85rem; text-transform:uppercase; letter-spacing:.04em; }
  label { display:block; margin:.75rem 0 .25rem; font-size:.9rem; }
  input[type=number], input[type=text] { width:100%; box-sizing:border-box; background:#262626;
         border:1px solid #525252; color:#f4f4f4; padding:.5rem .6rem; font-size:1rem; }
  .row { display:flex; align-items:center; gap:.5rem; margin:.5rem 0; }
  .row input[type=checkbox] { width:1.1rem; height:1.1rem; }
  .hint { color:#8d8d8d; font-size:.78rem; margin:.2rem 0 0; }
  button { background:#0f62fe; color:#fff; border:0; padding:.6rem 1.25rem; font-size:1rem; cursor:pointer; }
  button:disabled { opacity:.5; cursor:default; }
  #status { margin-top:1rem; font-size:.9rem; min-height:1.2em; }
  .ok { color:#42be65; } .err { color:#fa4d56; }
</style>
</head>
<body>
<main>
  <h1>LiveFabric Broadcast Box</h1>
  <p class="sub">Runtime settings — applied live and persisted across restarts.</p>

  <form id="f">
    <fieldset>
      <legend>Egress pacer</legend>
      <div class="row">
        <input type="checkbox" id="pacerEnabled">
        <label for="pacerEnabled" style="margin:0">Enable frame-aware pacing</label>
      </div>
      <label for="pacerMbps">Target rate (Mbps)</label>
      <input type="number" id="pacerMbps" min="0.1" max="100" step="0.1" value="1.5">
      <p class="hint">Set at or slightly above the encoder's average bitrate. Smooths bursts losslessly; sustained overload drops whole frames and resyncs on the next keyframe.</p>
    </fieldset>

    <fieldset>
      <legend>Keyframe requests</legend>
      <label for="pliMs">PLI throttle (ms)</label>
      <input type="number" id="pliMs" min="100" max="5000" step="50" value="750">
      <p class="hint">Minimum interval between keyframe requests forwarded to the encoder (100–5000 ms).</p>
    </fieldset>

    <fieldset>
      <legend>Networking</legend>
      <label for="natIp">NAT / advertised IP override</label>
      <input type="text" id="natIp" placeholder="auto-detected" inputmode="numeric">
      <p class="hint">Leave blank to auto-detect the LAN IP (recommended). Changing this applies after a container restart.</p>
    </fieldset>

    <fieldset>
      <legend>SSH diagnostics</legend>
      <div class="row">
        <input type="checkbox" id="sshEnabled">
        <label for="sshEnabled" style="margin:0">Enable on-box SSH (key-only, root)</label>
      </div>
      <label for="sshPort">SSH port</label>
      <input type="number" id="sshPort" min="1" max="65535" step="1" value="22">
      <p class="hint">Enabled by default. Under host networking this binds directly on the appliance IP and lands in the host net namespace (tcpdump/iftop/ss see real traffic). Use a non-22 port (e.g. 2222) if the host already runs sshd. Changes apply after a container restart.</p>
    </fieldset>

    <button type="submit" id="save">Save</button>
    <div id="status"></div>
  </form>
</main>
<script>
(function () {
  var $ = function (id) { return document.getElementById(id); };
  var status = $("status");

  function load() {
    fetch("/api/settings").then(function (r) { return r.json(); }).then(function (s) {
      $("pacerEnabled").checked = !!s.pacerEnabled;
      $("pacerMbps").value = s.pacerBps ? (s.pacerBps / 1e6) : 1.5;
      $("pliMs").value = s.pliThrottleMs || 750;
      $("natIp").value = s.natOverrideIp || "";
      $("sshEnabled").checked = s.sshEnabled !== false;
      $("sshPort").value = s.sshPort || 22;
    }).catch(function () { status.textContent = "Failed to load settings"; status.className = "err"; });
  }

  $("f").addEventListener("submit", function (e) {
    e.preventDefault();
    $("save").disabled = true;
    status.textContent = "Saving…"; status.className = "";
    var body = {
      pacerEnabled: $("pacerEnabled").checked,
      pacerBps: Math.round(parseFloat($("pacerMbps").value || "0") * 1e6),
      pliThrottleMs: parseInt($("pliMs").value || "750", 10),
      natOverrideIp: $("natIp").value.trim(),
      sshEnabled: $("sshEnabled").checked,
      sshPort: parseInt($("sshPort").value || "22", 10)
    };
    fetch("/api/settings", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body)
    }).then(function (r) { return r.json().then(function (j) { return { ok: r.ok, j: j }; }); })
      .then(function (res) {
        $("save").disabled = false;
        if (!res.ok) { status.textContent = res.j.error || "Save failed"; status.className = "err"; return; }
        // reflect the sanitised values the server actually stored
        $("pacerEnabled").checked = !!res.j.pacerEnabled;
        $("pacerMbps").value = res.j.pacerBps ? (res.j.pacerBps / 1e6) : 0;
        $("pliMs").value = res.j.pliThrottleMs;
        $("natIp").value = res.j.natOverrideIp || "";
        $("sshEnabled").checked = res.j.sshEnabled !== false;
        $("sshPort").value = res.j.sshPort || 22;
        status.textContent = "Saved."; status.className = "ok";
      }).catch(function () {
        $("save").disabled = false;
        status.textContent = "Save failed"; status.className = "err";
      });
  });

  load();
})();
</script>
</body>
</html>`
