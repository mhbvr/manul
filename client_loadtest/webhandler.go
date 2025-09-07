package main

import (
	"html/template"
	"log"
	"net/http"
	"strconv"
	"time"
)

type WebHandler struct {
	loadTester *LoadTester
	template   *template.Template
}

func NewWebHandler(loadTester *LoadTester) *WebHandler {
	funcMap := template.FuncMap{
		"div": func(a, b float64) float64 { 
			if b == 0 { return 0 }
			return a / b 
		},
		"mul": func(a, b float64) float64 { return a * b },
	}
	tmpl := template.Must(template.New("index").Funcs(funcMap).Parse(indexTemplate))
	return &WebHandler{
		loadTester: loadTester,
		template:   tmpl,
	}
}

func (wh *WebHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/":
		wh.handleIndex(w, r)
	case "/update":
		wh.handleUpdate(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (wh *WebHandler) handleIndex(w http.ResponseWriter, r *http.Request) {
	serverAddr, inflight, mode, rps, timeout, err := wh.loadTester.GetConfig()
	if err != nil {
		http.Error(w, "Failed to get config: "+err.Error(), http.StatusInternalServerError)
		return
	}
	
	totalReq, successReq, errorReq, currentRPS := wh.loadTester.GetStats()
	
	data := struct {
		ServerAddr   string
		Inflight     int
		Mode         string
		RPS          float64
		Timeout      string
		TotalReq     int64
		SuccessReq   int64
		ErrorReq     int64
		CurrentRPS   float64
	}{
		ServerAddr:  serverAddr,
		Inflight:    inflight,
		Mode:        mode,
		RPS:         rps,
		Timeout:     timeout.String(),
		TotalReq:    totalReq,
		SuccessReq:  successReq,
		ErrorReq:    errorReq,
		CurrentRPS:  currentRPS,
	}
	
	w.Header().Set("Content-Type", "text/html")
	if err := wh.template.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (wh *WebHandler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}
	
	// Update inflight
	if inflightStr := r.FormValue("inflight"); inflightStr != "" {
		if inflight, err := strconv.Atoi(inflightStr); err == nil && inflight > 0 {
			if err := wh.loadTester.SetInflight(inflight); err != nil {
				log.Printf("Failed to set inflight: %v", err)
			}
		}
	}
	
	// Update mode
	if mode := r.FormValue("mode"); mode != "" {
		if mode == "asap" || mode == "stable" || mode == "exponential" {
			if err := wh.loadTester.SetMode(mode); err != nil {
				log.Printf("Failed to set mode: %v", err)
			}
		}
	}
	
	// Update RPS
	if rpsStr := r.FormValue("rps"); rpsStr != "" {
		if rps, err := strconv.ParseFloat(rpsStr, 64); err == nil && rps >= 0 {
			if err := wh.loadTester.SetRPS(rps); err != nil {
				log.Printf("Failed to set RPS: %v", err)
			}
		}
	}
	
	// Update timeout
	if timeoutStr := r.FormValue("timeout"); timeoutStr != "" {
		if timeout, err := time.ParseDuration(timeoutStr); err == nil && timeout > 0 {
			if err := wh.loadTester.SetTimeout(timeout); err != nil {
				log.Printf("Failed to set timeout: %v", err)
			}
		}
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
    <meta http-equiv="refresh" content="5">
</head>https://github.com/mihkulemin/token
<body>
    <div class="container">
        <h1>Cat Photo Load Tester Control Panel</h1>
        
        <div class="section stats">
            <h2>Statistics</h2>
            <table>
                <tr><th>Total Requests</th><td>{{.TotalReq}}</td></tr>
                <tr><th>Successful Requests</th><td>{{.SuccessReq}}</td></tr>
                <tr><th>Failed Requests</th><td>{{.ErrorReq}}</td></tr>
                <tr><th>Current RPS</th><td>{{printf "%.2f" .CurrentRPS}}</td></tr>
            </table>
            <p><a href="/" class="refresh-link">Refresh Now</a></p>
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
                        <td><input type="number" name="inflight" value="{{.Inflight}}" min="1" max="1000"></td>
                    </tr>
                    <tr>
                        <th>Mode</th>
                        <td>
                            <select name="mode">
                                <option value="asap" {{if eq .Mode "asap"}}selected{{end}}>ASAP (Max Speed)</option>
                                <option value="stable" {{if eq .Mode "stable"}}selected{{end}}>Stable Interval</option>
                                <option value="exponential" {{if eq .Mode "exponential"}}selected{{end}}>Exponential Distribution</option>
                            </select>
                        </td>
                    </tr>
                    <tr>
                        <th>Target RPS</th>
                        <td><input type="number" name="rps" value="{{.RPS}}" min="0" max="10000" step="0.1"></td>
                    </tr>
                    <tr>
                        <th>Request Timeout</th>
                        <td><input type="text" name="timeout" value="{{.Timeout}}" placeholder="10s"></td>
                    </tr>
                </table>
                <button type="submit">Update Configuration</button>
            </form>
        </div>
        
        <div class="section">
            <h2>Usage</h2>
            <ul>
                <li><strong>In-Flight Requests:</strong> Current number of concurrent requests allowed</li>
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