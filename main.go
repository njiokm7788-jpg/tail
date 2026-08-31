package main

/*
#include <stdint.h>
#include <stdlib.h>

typedef struct {
	void* ptr;
	size_t len;
} cliproxy_buffer;

typedef int (*cliproxy_host_call_fn)(void*, const char*, const uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_host_free_fn)(void*, size_t);

typedef struct {
	uint32_t abi_version;
	void* host_ctx;
	cliproxy_host_call_fn call;
	cliproxy_host_free_fn free_buffer;
} cliproxy_host_api;
*/
import "C"

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"unsafe"
)

// ---------------------------------------------------------------------------
// Global state
// ---------------------------------------------------------------------------

var (
	hostAPI *C.cliproxy_host_api
	abiVer  uint32
)

// ---------------------------------------------------------------------------
// ABI exports
// ---------------------------------------------------------------------------

//export cliproxy_plugin_init
func cliproxy_plugin_init(CabiVer C.uint32_t, hostApi *C.cliproxy_host_api, response *C.cliproxy_buffer) C.int {
	abiVer = uint32(CabiVer)
	hostAPI = hostApi
	writeResponse(response, okJSON(registration()))
	return 0
}

//export cliproxy_plugin_call
func cliproxy_plugin_call(method *C.char, request *C.uint8_t, requestLen C.size_t, response *C.cliproxy_buffer) C.int {
	m := C.GoString(method)
	var req []byte
	if requestLen > 0 {
		req = C.GoBytes(unsafe.Pointer(request), C.int(requestLen))
	}
	resp := handleMethod(m, req)
	writeResponse(response, resp)
	return 0
}

//export cliproxy_plugin_free_buffer
func cliproxy_plugin_free_buffer(ptr unsafe.Pointer, length C.size_t) {
	C.free(ptr)
}

//export cliproxy_plugin_shutdown
func cliproxy_plugin_shutdown() {
	// No runtime to clean up; this plugin is a config generator only.
}

// ---------------------------------------------------------------------------
// Registration
// ---------------------------------------------------------------------------

func registration() map[string]interface{} {
	return map[string]interface{}{
		"schema_version": 4,
		"metadata": map[string]interface{}{
			"name":              "tailscale-gateway",
			"version":           "0.2.0",
			"author":            "user",
			"github_repository": "local",
		},
		"capabilities": map[string]interface{}{
			"management_api": true,
		},
	}
}

// ---------------------------------------------------------------------------
// Method dispatch
// ---------------------------------------------------------------------------

func handleMethod(method string, raw []byte) []byte {
	switch method {
	case "plugin.register", "plugin.reconfigure":
		return okJSON(registration())

	case "plugin.shutdown", "plugin.quiesce":
		return okJSON(map[string]string{"status": "shutdown"})

	case "management.register":
		return okJSON(map[string]interface{}{
			"routes": []map[string]interface{}{
				{
					"path":        "/page",
					"method":      "GET",
					"menu":        "Tailscale Gateway",
					"description": "Tailscale configuration generator",
				},
				{
					"path":        "/generate",
					"method":      "POST",
					"description": "Generate config YAML from form data",
				},
			},
		})

	case "management.handle":
		return handleManagement(raw)

	default:
		return okJSON(map[string]string{})
	}
}

// ---------------------------------------------------------------------------
// Management routes
// ---------------------------------------------------------------------------

func handleManagement(raw []byte) []byte {
	// raw JSON: {"path":"/page","method":"GET","body":"..."}
	var req struct {
		Path   string `json:"path"`
		Method string `json:"method"`
		Body   string `json:"body"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return errJSON("bad_request", err.Error())
	}

	switch {
	case req.Path == "/page" && strings.EqualFold(req.Method, "GET"):
		return managementResponse(http.StatusOK, "text/html; charset=utf-8", pageHTML)

	case req.Path == "/generate" && strings.EqualFold(req.Method, "POST"):
		return handleGenerate(req.Body)

	default:
		return errJSON("not_found", "unknown management path: "+req.Path)
	}
}

// ---------------------------------------------------------------------------
// POST /generate — parse form input, emit YAML snippet
// ---------------------------------------------------------------------------

func handleGenerate(body string) []byte {
	var input struct {
		AuthKey    string `json:"auth_key"`
		Hostname   string `json:"hostname"`
		SocksPort  int    `json:"socks_port"`
		Target     string `json:"target"`
		ProxyURL   string `json:"proxy_url"`
	}
	if err := json.Unmarshal([]byte(body), &input); err != nil {
		return errJSON("bad_request", err.Error())
	}

	// Defaults
	if input.Hostname == "" {
		input.Hostname = "cpa-docker"
	}
	if input.SocksPort == 0 {
		input.SocksPort = 18080
	}
	if input.Target == "" {
		input.Target = "100.x.x.x:port"
	}
	if input.ProxyURL == "" {
		input.ProxyURL = fmt.Sprintf("socks5://127.0.0.1:%d", input.SocksPort)
	}
	if input.AuthKey == "" {
		return errJSON("bad_request", "auth_key is required")
	}

	// Build YAML snippet
	var b strings.Builder
	b.WriteString("# ===== Tailscale Gateway Plugin Config =====\n")
	b.WriteString("# Copy this into your config.yaml and restart CPA.\n")
	b.WriteString("# In each auth record, set:\n")
	b.WriteString("#   proxy-url: \"")
	b.WriteString(input.ProxyURL)
	b.WriteString("\"\n")
	b.WriteString("# and configure the proxy pool target via the plugin.\n\n")
	b.WriteString("plugins:\n")
	b.WriteString("  enabled: true\n")
	b.WriteString("  dir: plugins\n")
	b.WriteString("  configs:\n")
	b.WriteString("    tailscale-gateway:\n")
	b.WriteString("      enabled: true\n")
	b.WriteString("      priority: 1\n")
	b.WriteString(fmt.Sprintf("      auth_key: %q\n", input.AuthKey))
	b.WriteString(fmt.Sprintf("      hostname: %q\n", input.Hostname))
	b.WriteString(fmt.Sprintf("      socks_port: %d\n", input.SocksPort))
	b.WriteString(fmt.Sprintf("      target: %q\n", input.Target))
	b.WriteString("\n# --- Auth record example ---\n")
	b.WriteString("# In your auth files (e.g. claude_auth.yaml), add:\n")
	b.WriteString("# proxy-url: \"")
	b.WriteString(input.ProxyURL)
	b.WriteString("\"\n")

	raw, _ := json.Marshal(map[string]interface{}{
		"ok":     true,
		"yaml":   b.String(),
		"proxy":  input.ProxyURL,
		"target": input.Target,
	})
	return raw
}

// ---------------------------------------------------------------------------
// HTML page — form for inputting Tailscale config
// ---------------------------------------------------------------------------

const pageHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Tailscale Gateway</title>
<style>
  :root { --bg: #0d1117; --card: #161b22; --border: #30363d; --text: #e6edf3; --muted: #8b949e; --accent: #58a6ff; --green: #3fb950; --red: #f85149; }
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Helvetica, Arial, sans-serif; background: var(--bg); color: var(--text); min-height: 100vh; display: flex; justify-content: center; padding: 2rem; }
  .container { max-width: 700px; width: 100%; }
  h1 { font-size: 1.5rem; margin-bottom: 0.5rem; }
  h1 span { color: var(--accent); }
  .subtitle { color: var(--muted); font-size: 0.85rem; margin-bottom: 1.5rem; }
  .card { background: var(--card); border: 1px solid var(--border); border-radius: 8px; padding: 1.5rem; margin-bottom: 1rem; }
  .field { margin-bottom: 1rem; }
  label { display: block; font-size: 0.85rem; color: var(--muted); margin-bottom: 0.3rem; }
  input { width: 100%; padding: 0.6rem 0.8rem; background: var(--bg); border: 1px solid var(--border); border-radius: 6px; color: var(--text); font-size: 0.9rem; font-family: 'SF Mono', Consolas, monospace; }
  input:focus { outline: none; border-color: var(--accent); }
  .row { display: flex; gap: 1rem; }
  .row .field { flex: 1; }
  .hint { font-size: 0.75rem; color: var(--muted); margin-top: 0.2rem; }
  button { width: 100%; padding: 0.7rem; background: var(--accent); color: #fff; border: none; border-radius: 6px; font-size: 0.9rem; font-weight: 600; cursor: pointer; margin-top: 0.5rem; }
  button:hover { opacity: 0.9; }
  .result { display: none; margin-top: 1rem; }
  .result.show { display: block; }
  .result-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 0.5rem; }
  .result-header span { font-size: 0.85rem; color: var(--green); }
  pre { background: var(--bg); border: 1px solid var(--border); border-radius: 6px; padding: 1rem; overflow-x: auto; font-size: 0.8rem; line-height: 1.5; white-space: pre-wrap; word-break: break-all; max-height: 400px; overflow-y: auto; }
  .copy-btn { background: none; border: 1px solid var(--border); color: var(--muted); padding: 0.3rem 0.7rem; border-radius: 4px; cursor: pointer; font-size: 0.75rem; width: auto; margin: 0; }
  .copy-btn:hover { border-color: var(--accent); color: var(--accent); }
  .note { background: #1c2333; border-left: 3px solid var(--accent); padding: 0.8rem 1rem; border-radius: 0 6px 6px 0; font-size: 0.8rem; color: var(--muted); margin-bottom: 1rem; }
</style>
</head>
<body>
<div class="container">
  <h1><span>Tailscale</span> Gateway</h1>
  <p class="subtitle">Generate plugin configuration for CPA Tailscale integration.</p>

  <div class="note">
    This plugin generates a YAML config snippet. Copy it into your <code>config.yaml</code> and restart CPA.
    Each auth record needs <code>proxy-url: "socks5://127.0.0.1:&lt;port&gt;"</code> to route through the gateway.
  </div>

  <div class="card">
    <div class="field">
      <label>Auth Key</label>
      <input id="auth_key" type="text" placeholder="tskey-auth-xxxxxxxxxxxx" autocomplete="off">
      <div class="hint">Generate at <a href="https://login.tailscale.com/admin/settings/keys" target="_blank" style="color:var(--accent)">Tailscale Admin → Keys</a>. Use a Reusable key.</div>
    </div>
    <div class="row">
      <div class="field">
        <label>Hostname</label>
        <input id="hostname" type="text" placeholder="cpa-docker" value="cpa-docker">
      </div>
      <div class="field">
        <label>Socks5 Port</label>
        <input id="socks_port" type="number" placeholder="18080" value="18080">
      </div>
    </div>
    <div class="field">
      <label>Proxy Pool Target (tailnet address)</label>
      <input id="target" type="text" placeholder="100.x.x.x:1080">
      <div class="hint">The proxy pool address inside your tailnet. Check Tailscale admin console for IPs.</div>
    </div>
    <button onclick="generate()">Generate Config</button>
  </div>

  <div class="card result" id="result">
    <div class="result-header">
      <span>✓ Generated Config</span>
      <button class="copy-btn" onclick="copyYaml()">Copy to Clipboard</button>
    </div>
    <pre id="yaml_output"></pre>
    <div class="hint" style="margin-top:0.5rem">
      After pasting into <code>config.yaml</code>, also set <code>proxy-url</code> in each auth record:
      <br><code id="proxy_hint"></code>
    </div>
  </div>
</div>

<script>
function generate() {
  var data = {
    auth_key: document.getElementById('auth_key').value.trim(),
    hostname: document.getElementById('hostname').value.trim(),
    socks_port: parseInt(document.getElementById('socks_port').value) || 18080,
    target: document.getElementById('target').value.trim()
  };
  if (!data.auth_key) { alert('Auth Key is required'); return; }

  var xhr = new XMLHttpRequest();
  xhr.open('POST', '/v0/management/tailscale-gateway/generate', true);
  xhr.setRequestHeader('Content-Type', 'application/json');
  xhr.onreadystatechange = function() {
    if (xhr.readyState === 4) {
      var resp = JSON.parse(xhr.responseText);
      if (resp.ok) {
        document.getElementById('yaml_output').textContent = resp.yaml;
        document.getElementById('proxy_hint').textContent = 'proxy-url: "' + resp.proxy + '"';
        document.getElementById('result').classList.add('show');
      } else {
        alert('Error: ' + resp.error.message);
      }
    }
  };
  xhr.send(JSON.stringify(data));
}

function copyYaml() {
  var text = document.getElementById('yaml_output').textContent;
  navigator.clipboard.writeText(text).then(function() {
    var btn = document.querySelector('.copy-btn');
    btn.textContent = 'Copied!';
    setTimeout(function() { btn.textContent = 'Copy to Clipboard'; }, 1500);
  });
}
</script>
</body>
</html>`

// ---------------------------------------------------------------------------
// Response helpers
// ---------------------------------------------------------------------------

func okJSON(result interface{}) []byte {
	raw, _ := json.Marshal(map[string]interface{}{
		"ok":     true,
		"result": result,
	})
	return raw
}

func errJSON(code, message string) []byte {
	raw, _ := json.Marshal(map[string]interface{}{
		"ok": false,
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
	return raw
}

func managementResponse(statusCode int, contentType string, body string) []byte {
	raw, _ := json.Marshal(map[string]interface{}{
		"ok":          true,
		"status_code": statusCode,
		"headers":     map[string]string{"Content-Type": contentType},
		"body":        body,
	})
	return raw
}

func writeResponse(response *C.cliproxy_buffer, raw []byte) {
	if response == nil || len(raw) == 0 {
		return
	}
	ptr := C.CBytes(raw)
	if ptr == nil {
		return
	}
	response.ptr = ptr
	response.len = C.size_t(len(raw))
}

func main() {}
