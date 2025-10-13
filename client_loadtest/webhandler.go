package main

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strconv"
	"time"

	"go.opentelemetry.io/contrib/zpages"
)

type WebHandler struct {
	loadTester      *LoadTester
	template        *template.Template
	zpagesProcessor *zpages.SpanProcessor
}

func NewWebHandler(loadTester *LoadTester, zpagesProcessor *zpages.SpanProcessor) *WebHandler {
	funcMap := template.FuncMap{
		"add": func(a, b int) int {
			return a + b
		},
	}
	tmpl := template.Must(template.New("index").Funcs(funcMap).Parse(indexTemplate))
	return &WebHandler{
		loadTester:      loadTester,
		template:        tmpl,
		zpagesProcessor: zpagesProcessor,
	}
}

func (wh *WebHandler) HandleIndex(w http.ResponseWriter, r *http.Request) {
	info, err := wh.loadTester.GetRunnersInfo(r.Context())
	if err != nil {
		http.Error(w, "Failed to get runners info: "+err.Error(), http.StatusInternalServerError)
		return
	}

	data := struct {
		MaxInFlight int
		LoadTypes   []string
		RunnerInfo  []*Status
	}{
		MaxInFlight: wh.loadTester.GetMaxInFlight(),
		LoadTypes:   wh.loadTester.GetAvailableLoadTypes(),
		RunnerInfo:  info,
	}

	w.Header().Set("Content-Type", "text/html")
	if err := wh.template.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (wh *WebHandler) HandleAddRunner(w http.ResponseWriter, r *http.Request) {
	var err error

	if err = r.ParseForm(); err != nil {
		http.Error(w, "Failed to parse form: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Parse load type
	loadType := r.FormValue("load_type")
	if loadType == "" {
		http.Error(w, "load_type is required", http.StatusBadRequest)
		return
	}

	// Get available options for this load type
	availableOptions, err := wh.loadTester.GetLoadOptions(loadType)
	if err != nil {
		http.Error(w, "Invalid load type: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Parse load options from form
	loadOptions := make(map[string]string)
	for optionName := range availableOptions {
		if value := r.FormValue(optionName); value != "" {
			loadOptions[optionName] = value
		}
	}

	// Parse inflight
	var inFlight int = 1 // default
	if inflightStr := r.FormValue("inflight"); inflightStr != "" {
		if inFlight, err = strconv.Atoi(inflightStr); err != nil {
			http.Error(w, "Failed to parse inflight: "+err.Error(), http.StatusBadRequest)
			return
		}
	}

	// Parse mode
	mode := r.FormValue("mode")
	if mode == "" {
		mode = "asap" // default
	}

	// Parse QPS
	var qps float64 = 1.0 // default
	if qpsStr := r.FormValue("qps"); qpsStr != "" {
		if qps, err = strconv.ParseFloat(qpsStr, 64); err != nil {
			http.Error(w, "Failed to parse qps: "+err.Error(), http.StatusBadRequest)
			return
		}
	}

	// Parse timeout
	var timeout time.Duration = 10 * time.Second // default
	if timeoutStr := r.FormValue("timeout"); timeoutStr != "" {
		if timeout, err = time.ParseDuration(timeoutStr); err != nil {
			http.Error(w, "Failed to parse timeout: "+err.Error(), http.StatusBadRequest)
			return
		}
	}

	if err := wh.loadTester.AddRunner(loadType, loadOptions, inFlight, qps, timeout, mode); err != nil {
		http.Error(w, "Failed to add runner: "+err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (wh *WebHandler) HandleGetLoadOptions(w http.ResponseWriter, r *http.Request) {
	loadType := r.URL.Query().Get("type")
	if loadType == "" {
		http.Error(w, "type parameter is required", http.StatusBadRequest)
		return
	}

	options, err := wh.loadTester.GetLoadOptions(loadType)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(options)
}

func (wh *WebHandler) HandleRemoveRunner(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Failed to parse form: "+err.Error(), http.StatusBadRequest)
		return
	}

	runnerID := r.FormValue("runner_id")
	if runnerID == "" {
		http.Error(w, "runner_id is required", http.StatusBadRequest)
		return
	}

	if err := wh.loadTester.RemoveRunner(runnerID); err != nil {
		http.Error(w, "Failed to remove runner: "+err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (wh *WebHandler) HandleUpdateRunner(w http.ResponseWriter, r *http.Request) {
	var err error

	if err = r.ParseForm(); err != nil {
		http.Error(w, "Failed to parse form: "+err.Error(), http.StatusBadRequest)
		return
	}

	runnerID := r.FormValue("runner_id")
	if runnerID == "" {
		http.Error(w, "runner_id is required", http.StatusBadRequest)
		return
	}

	// Parse inflight
	var inFlight int
	if inflightStr := r.FormValue("inflight"); inflightStr != "" {
		if inFlight, err = strconv.Atoi(inflightStr); err != nil {
			http.Error(w, "Failed to parse inflight: "+err.Error(), http.StatusBadRequest)
			return
		}
	}

	// Parse mode
	mode := r.FormValue("mode")
	if mode == "" {
		http.Error(w, "mode is empty", http.StatusBadRequest)
		return
	}

	// Parse QPS
	var qps float64
	if qpsStr := r.FormValue("qps"); qpsStr != "" {
		if qps, err = strconv.ParseFloat(qpsStr, 64); err != nil {
			http.Error(w, "Failed to parse qps: "+err.Error(), http.StatusBadRequest)
			return
		}
	}

	// Parse timeout
	var timeout time.Duration
	if timeoutStr := r.FormValue("timeout"); timeoutStr != "" {
		if timeout, err = time.ParseDuration(timeoutStr); err != nil {
			http.Error(w, "Failed to parse timeout: "+err.Error(), http.StatusBadRequest)
			return
		}
	}

	if err := wh.loadTester.UpdateRunner(runnerID, inFlight, qps, timeout, mode); err != nil {
		http.Error(w, "Failed to update runner: "+err.Error(), http.StatusInternalServerError)
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
        .container { max-width: 1200px; margin: 0 auto; }
        .section { margin: 20px 0; padding: 15px; border: 1px solid #ddd; border-radius: 5px; overflow-x: auto; }
        .stats { background-color: #f5f5f5; }
        .controls { background-color: #fff; }
        table { width: 100%; border-collapse: collapse; }
        th, td { text-align: left; padding: 8px; border-bottom: 1px solid #ddd; font-size: 14px; }
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
            <h2>Runner Management ({{len .RunnerInfo}} active)</h2>
            <div style="margin-bottom: 15px;">
                <button type="button" onclick="showAddForm()">Add New Runner</button>
            </div>
            
            <div class="section controls" id="add-form" style="display: none; margin-bottom: 20px;">
                <h3>Add New Runner</h3>
                <form method="post" action="/add-runner" id="add-runner-form">
                    <table>
                        <tr>
                            <th>In-Flight Requests</th>
                            <td>
                                <input type="number" name="inflight" value="1" min="0" max="{{.MaxInFlight}}">
                                <em style="font-size: 0.9em; color: #666; margin-left: 10px;">(max: {{.MaxInFlight}}, shared across all runners)</em>
                            </td>
                        </tr>
                        <tr>
                            <th>Mode</th>
                            <td>
                                <select name="mode">
                                    <option value="asap" selected>ASAP (Max Speed)</option>
                                    <option value="static">Static Interval</option>
                                    <option value="exponential">Exponential Distribution</option>
                                </select>
                            </td>
                        </tr>
                        <tr>
                            <th>Target QPS</th>
                            <td><input type="number" name="qps" value="1.0" min="0" step="0.1"></td>
                        </tr>
                        <tr>
                            <th>Request Timeout</th>
                            <td><input type="text" name="timeout" value="10s"></td>
                        </tr>
                        <tr>
                            <th>Load Type</th>
                            <td>
                                <select name="load_type" id="load-type-select" onchange="loadOptionsForType(this.value)" required>
                                    <option value="">-- Select Load Type --</option>
                                    {{range .LoadTypes}}
                                    <option value="{{.}}">{{.}}</option>
                                    {{end}}
                                </select>
                            </td>
                        </tr>
                    </table>
                    <div id="load-options-container"></div>
                    <button type="submit">Create Runner</button>
                    <button type="button" onclick="hideAddForm()">Cancel</button>
                </form>
            </div>
            <table>
                <thead>
                    <tr>
                        <th>Runner ID</th>
                        <th>Load Options</th>
                        <th>Start Time</th>
                        <th>In-Flight</th>
                        <th>Mode</th>
                        <th>QPS</th>
                        <th>Timeout</th>
                        <th>Successful</th>
                        <th>Failed</th>
                        <th>Actions</th>
                    </tr>
                </thead>
                <tbody>
                    {{range .RunnerInfo}}
                    <tr>
                        <td>{{.Id}}</td>
                        <td style="font-size: 0.85em;">
                            {{if .LoadOptions}}
                                {{range $key, $value := .LoadOptions}}
                                    <div><strong>{{$key}}:</strong> {{$value}}</div>
                                {{end}}
                            {{else}}
                                <em>none</em>
                            {{end}}
                        </td>
                        <td>{{.LoadRunnerInfo.StartTime.Format "15:04:05"}}</td>
                        <td>{{.LoadRunnerInfo.WorkerCfg.InFlight}}</td>
                        <td>{{.Mode}}</td>
                        <td>{{if eq .Mode "asap"}}-{{else}}{{.LoadRunnerInfo.WorkerCfg.Qps}}{{end}}</td>
                        <td>{{.LoadRunnerInfo.WorkerCfg.Timeout}}</td>
                        <td>{{.OkRequests}}</td>
                        <td>{{.ErrRequests}}</td>
                        <td style="white-space: nowrap;">
                            <button type="button" onclick="showEditForm('{{.Id}}', {{.LoadRunnerInfo.WorkerCfg.InFlight}}, '{{.Mode}}', {{.LoadRunnerInfo.WorkerCfg.Qps}}, '{{.LoadRunnerInfo.WorkerCfg.Timeout}}')" style="margin-right: 10px;">Edit</button><button type="submit" form="remove-form-{{.Id}}" onclick="return confirm('Remove runner {{.Id}}?')">Remove</button>
                            <form id="remove-form-{{.Id}}" method="post" action="/remove-runner" style="display: none;">
                                <input type="hidden" name="runner_id" value="{{.Id}}">
                            </form>
                        </td>
                    </tr>
                    {{end}}
                </tbody>
            </table>
            <p><a href="/" class="refresh-link">Refresh Now</a> | <a href="/metrics" class="refresh-link">Prometheus Metrics</a> | <a href="/tracez" class="refresh-link">Traces</a></p>
        </div>
        
        <div class="section controls" id="edit-form" style="display: none;">
            <h2>Edit Runner Configuration</h2>
            <form method="post" action="/update-runner">
                <input type="hidden" id="edit-runner-id" name="runner_id" value="">
                <table>
                    <tr>
                        <th>Runner ID</th>
                        <td id="edit-runner-display"></td>
                    </tr>
                    <tr>
                        <th>In-Flight Requests</th>
                        <td><input type="number" id="edit-inflight" name="inflight" min="0" max="{{.MaxInFlight}}"></td>
                    </tr>
                    <tr>
                        <th>Mode</th>
                        <td>
                            <select id="edit-mode" name="mode">
                                <option value="asap">ASAP (Max Speed)</option>
                                <option value="static">Static Interval</option>
                                <option value="exponential">Exponential Distribution</option>
                            </select>
                        </td>
                    </tr>
                    <tr>
                        <th>Target QPS</th>
                        <td><input type="number" id="edit-qps" name="qps" min="0" step="0.1"></td>
                    </tr>
                    <tr>
                        <th>Request Timeout</th>
                        <td><input type="text" id="edit-timeout" name="timeout"></td>
                    </tr>
                </table>
                <button type="submit">Update Runner</button>
                <button type="button" onclick="hideEditForm()">Cancel</button>
            </form>
        </div>
        
        <div class="section">
            <h2>Usage</h2>
            <ul>
                <li><strong>Add Runner:</strong> Click "Add New Runner" to create a new runner with default configuration</li>
                <li><strong>Edit Runner:</strong> Click "Edit" next to any runner to modify its configuration</li>
                <li><strong>Remove Runner:</strong> Click "Remove" to delete a runner (confirmation required)</li>
                <li><strong>Server Address:</strong> Use traditional addresses (localhost:8081) or Kubernetes services (k8s://my-service.default:8080)</li>
                <li><strong>In-Flight Requests:</strong> Per-runner limit of concurrent requests allowed</li>
                <li><strong>ASAP Mode:</strong> Send requests as fast as possible (limited only by In-Flight)</li>
                <li><strong>Static Interval:</strong> Send requests at regular intervals based on Target QPS</li>
                <li><strong>Exponential Distribution:</strong> Send requests with exponentially distributed intervals (average = Target QPS)</li>
                <li><strong>Request Timeout:</strong> Maximum time to wait for each request (e.g., "10s", "500ms")</li>
                <li><strong>Prometheus Metrics:</strong> Metrics are labeled with runner_id for per-runner analysis</li>
            </ul>
        </div>
    </div>
    
    <script>
        function showAddForm() {
            document.getElementById('add-form').style.display = 'block';
        }

        function hideAddForm() {
            document.getElementById('add-form').style.display = 'none';
            document.getElementById('load-options-container').innerHTML = '';
            document.getElementById('load-type-select').value = '';
        }

        function loadOptionsForType(loadType) {
            const container = document.getElementById('load-options-container');

            if (!loadType) {
                container.innerHTML = '';
                return;
            }

            fetch('/api/load-options?type=' + encodeURIComponent(loadType))
                .then(response => response.json())
                .then(options => {
                    let html = '<table><tr><th colspan="2" style="background-color: #f0f0f0;">Load-Specific Options</th></tr>';

                    for (const [key, description] of Object.entries(options)) {
                        html += '<tr><th>' + key + '</th>';
                        html += '<td><input type="text" name="' + key + '" placeholder="' + description + '" style="width: 100%;"></td></tr>';
                    }

                    html += '</table>';
                    container.innerHTML = html;
                })
                .catch(error => {
                    container.innerHTML = '<p style="color: red;">Error loading options: ' + error + '</p>';
                });
        }

        function showEditForm(runnerId, inflight, mode, qps, timeout) {
            // Hide add form if it's open
            hideAddForm();

            document.getElementById('edit-runner-id').value = runnerId;
            document.getElementById('edit-runner-display').textContent = runnerId;
            document.getElementById('edit-inflight').value = inflight;
            document.getElementById('edit-mode').value = mode;
            document.getElementById('edit-qps').value = qps;
            document.getElementById('edit-timeout').value = timeout;
            document.getElementById('edit-form').style.display = 'block';
            document.getElementById('edit-form').scrollIntoView({ behavior: 'smooth' });
        }

        function hideEditForm() {
            document.getElementById('edit-form').style.display = 'none';
        }
    </script>
</body>
</html>
`
