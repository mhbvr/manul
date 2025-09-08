package main

import (
	"html/template"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type WebHandler struct {
	loadTester *LoadTester
	template   *template.Template
}

func NewWebHandler(loadTester *LoadTester) *WebHandler {
	tmpl := template.Must(template.New("index").Parse(indexTemplate))
	return &WebHandler{
		loadTester: loadTester,
		template:   tmpl,
	}
}

func (wh *WebHandler) HttpMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", wh.handleIndex)
	mux.HandleFunc("POST /update", wh.handleUpdate)
	mux.Handle("GET /metrics", promhttp.Handler())
	return mux
}

func (wh *WebHandler) handleIndex(w http.ResponseWriter, r *http.Request) {
	info, err := wh.loadTester.GetInfo(r.Context())
	if err != nil {
		http.Error(w, "Failed to get loadtester info: "+err.Error(), http.StatusInternalServerError)
		return
	}

	data := struct {
		ServerAddr  string
		MaxInFlight int
		RunnerInfo  *RunnerInfo
	}{
		ServerAddr:  wh.loadTester.serverAddr,
		MaxInFlight: wh.loadTester.maxInflight,
		RunnerInfo:  info,
	}

	w.Header().Set("Content-Type", "text/html")
	if err := wh.template.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (wh *WebHandler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	var err error

	if err = r.ParseForm(); err != nil {
		http.Error(w, "Failed to parse form: "+err.Error(), http.StatusBadRequest)
		return
	}

	cfg := &RunnerConfig{}

	// Update inflight
	if inflightStr := r.FormValue("inflight"); inflightStr != "" {
		if cfg.Inflight, err = strconv.Atoi(inflightStr); err != nil {
			http.Error(w, "Failed to parse inflight: "+err.Error(), http.StatusBadRequest)
			return
		}
	}

	cfg.Mode = r.FormValue("mode")
	if cfg.Mode == "" {
		http.Error(w, "mode is empty", http.StatusBadRequest)
		return
	}

	// Update RPS
	if rpsStr := r.FormValue("rps"); rpsStr != "" {
		if cfg.Rps, err = strconv.ParseFloat(rpsStr, 64); err != nil {
			http.Error(w, "Failed to parse rps: "+err.Error(), http.StatusBadRequest)
			return
		}
	}

	// Update timeout
	if timeoutStr := r.FormValue("timeout"); timeoutStr != "" {
		if cfg.Timeout, err = time.ParseDuration(timeoutStr); err != nil {
			http.Error(w, "Failed to parse timeout: "+err.Error(), http.StatusBadRequest)
			return
		}
	}

	err = wh.loadTester.SetConfig(r.Context(), cfg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

const indexTemplate = `
<!DOCTYPE html>
<html>
<head>
    <title>Load Tester Control Panel</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; }
        .container { max-width: 800px; margin: 0 auto; }
        .section { margin: 20px 0; padding: 15px; border: 1px solid #ddd; border-radius: 5px; }
        .stats { background-color: #f5f5f5; }
        .controls { background-color: #fff; }
        table { width: 100%; border-collapse: collapse; }
        th, td { text-align: left; padding: 8px; border-bottom: 1px solid #ddd; }
        input, select { margin: 5px; padding: 5px; }
        button { background-color: #007cba; color: white; padding: 10px 20px; border: none; border-radius: 3px; cursor: pointer; }
        button:hover { background-color: #005a87; }
        .refresh-link { color: #007cba; text-decoration: none; }
        .refresh-link:hover { text-decoration: underline; }
    </style>
</head>
<body>
    <div class="container">
        <h1>Cat Photo Load Tester Control Panel</h1>
        
        <div class="section stats">
            <h2>Statistics</h2>
            <table>
                <tr><th>Start time</th><td>{{.RunnerInfo.StartTime.Format "02 Jan, 2006 15:04:05 UTC"}}</td></tr>
                <tr><th>Successful Requests</th><td>{{.RunnerInfo.OkRequests}}</td></tr>
                <tr><th>Failed Requests</th><td>{{.RunnerInfo.ErrRequests}}</td></tr>
            </table>
            <p><a href="/" class="refresh-link">Refresh Now</a> | <a href="/metrics" class="refresh-link">Prometheus Metrics</a></p>
        </div>
        
        <div class="section controls">
            <h2>Configuration</h2>
            <form method="post" action="/update">
                <table>
                    <tr>
                        <th>Server Address</th>
                        <td>{{.ServerAddr}} (read-only)</td>
                    </tr>
                    <tr>
                        <th>In-Flight Requests</th>
                        <td><input type="number" name="inflight" value="{{.RunnerInfo.RunnerCfg.Inflight}}" min="0" max="{{.MaxInFlight}}"></td>
                    </tr>
                    <tr>
                        <th>Mode</th>
                        <td>
                            <select name="mode">
                                <option value="asap" {{if eq .RunnerInfo.RunnerCfg.Mode "asap"}}selected{{end}}>ASAP (Max Speed)</option>
                                <option value="stable" {{if eq .RunnerInfo.RunnerCfg.Mode "stable"}}selected{{end}}>Stable Interval</option>
                                <option value="exponential" {{if eq .RunnerInfo.RunnerCfg.Mode "exponential"}}selected{{end}}>Exponential Distribution</option>
                            </select>
                        </td>
                    </tr>
                    <tr>
                        <th>Target RPS</th>
                        <td><input type="number" name="rps" value="{{.RunnerInfo.RunnerCfg.Rps}}" min="0" step="0.1"></td>
                    </tr>
                    <tr>
                        <th>Request Timeout</th>
                        <td><input type="text" name="timeout" value="{{.RunnerInfo.RunnerCfg.Timeout}}"></td>
                    </tr>
                </table>
                <button type="submit">Update Configuration</button>
            </form>
        </div>
        
        <div class="section">
            <h2>Usage</h2>
            <ul>
                <li><strong>In-Flight Requests:</strong> Current limit of concurrent requests allowed</li>
                <li><strong>ASAP Mode:</strong> Send requests as fast as possible (limited only by In-Flight)</li>
                <li><strong>Stable Interval:</strong> Send requests at regular intervals based on Target RPS</li>
                <li><strong>Exponential Distribution:</strong> Send requests with exponentially distributed intervals (average = Target RPS)</li>
                <li><strong>Request Timeout:</strong> Maximum time to wait for each request (e.g., "10s", "500ms")</li>
            </ul>
        </div>
    </div>
</body>
</html>
`
