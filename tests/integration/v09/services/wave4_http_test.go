package services_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/files"
	"serverfleet.local/serverfleet/internal/monitoring"
	"serverfleet.local/serverfleet/internal/publishing"
)

func waveDigest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func waveUploadBytes(t *testing.T, url, subject string, content []byte) waveResult {
	t.Helper()
	request, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Test-Subject", subject)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var buffer bytes.Buffer
	if _, err := buffer.ReadFrom(response.Body); err != nil {
		t.Fatal(err)
	}
	return waveResult{Status: response.StatusCode, Body: buffer.Bytes()}
}

func TestWave4ServiceHTTPEndToEnd(t *testing.T) {
	env := newWaveEnv(t)
	base := env.server.URL

	t.Run("monitoring probe history", func(t *testing.T) {
		created := waveDo(t, base+"/api/v1/monitoring/availability-monitors", http.MethodPost, map[string]any{
			"name": "Wave 4 API", "type": "http", "target": "https://example.com/health",
			"intervalSeconds": 60, "timeoutSeconds": 5,
		}, "admin")
		waveExpect(t, created, http.StatusCreated)
		var monitor struct {
			ID string `json:"id"`
		}
		waveDecode(t, created, &monitor)

		for attempt := 0; attempt < 2; attempt++ {
			probe := waveDo(t, base+"/api/v1/monitoring/availability-monitors/"+monitor.ID+"/probe", http.MethodPost, nil, "admin")
			waveExpect(t, probe, http.StatusOK)
			var probeBody struct {
				MonitorID string `json:"monitorId"`
				Status    string `json:"status"`
				State     string `json:"state"`
				CheckedAt string `json:"checkedAt"`
			}
			waveDecode(t, probe, &probeBody)
			if probeBody.MonitorID != monitor.ID || probeBody.Status != "up" || probeBody.State != "up" || probeBody.CheckedAt == "" {
				t.Fatalf("probe = %+v", probeBody)
			}
		}

		history := waveDo(t, base+"/api/v1/monitoring/availability-monitors/"+monitor.ID+"/history?limit=1", http.MethodGet, nil, "admin")
		waveExpect(t, history, http.StatusOK)
		var page struct {
			MonitorID string `json:"monitorId"`
			Items     []struct {
				Status string `json:"status"`
			} `json:"items"`
			Limit int `json:"limit"`
		}
		waveDecode(t, history, &page)
		if page.MonitorID != monitor.ID || page.Limit != 1 || len(page.Items) != 1 || page.Items[0].Status != "up" {
			t.Fatalf("history = %+v", page)
		}
		alias := waveDo(t, base+"/api/v1/availability-monitors/"+monitor.ID+"/history", http.MethodGet, nil, "admin")
		waveExpect(t, alias, http.StatusOK)
		waveDecode(t, alias, &page)
		if len(page.Items) != 2 {
			t.Fatalf("alias history = %+v", page)
		}

		if badLimit := waveDo(t, base+"/api/v1/monitoring/availability-monitors/"+monitor.ID+"/history?limit=501", http.MethodGet, nil, "admin"); badLimit.Status != http.StatusBadRequest {
			t.Fatalf("invalid limit = %d", badLimit.Status)
		}
		if missing := waveDo(t, base+"/api/v1/monitoring/availability-monitors/missing/probe", http.MethodPost, nil, "admin"); missing.Status != http.StatusNotFound {
			t.Fatalf("missing probe = %d body=%s", missing.Status, missing.Body)
		}
		if denied := waveDo(t, base+"/api/v1/monitoring/availability-monitors/"+monitor.ID+"/history", http.MethodGet, nil, "intruder"); denied.Status != http.StatusForbidden {
			t.Fatalf("intruder history = %d", denied.Status)
		}
		if anonymous := waveDo(t, base+"/api/v1/monitoring/availability-monitors/"+monitor.ID+"/probe", http.MethodPost, nil, "anonymous"); anonymous.Status != http.StatusUnauthorized {
			t.Fatalf("anonymous probe = %d", anonymous.Status)
		}
	})

	t.Run("file upload bytes and checksum", func(t *testing.T) {
		content := []byte("wave4-staged-upload-content")
		digest := waveDigest(content)
		created := waveDo(t, base+"/api/v1/nodes/n1/files/uploads", http.MethodPost, map[string]any{
			"destinationDir": "/srv/app",
			"files":          []map[string]any{{"name": "wave4.bin", "sizeBytes": len(content), "sha256": digest}},
		}, "admin")
		waveExpect(t, created, http.StatusCreated)
		var session struct {
			UploadID string `json:"uploadId"`
		}
		waveDecode(t, created, &session)
		contentURL := base + "/api/v1/nodes/n1/files/uploads/" + session.UploadID + "/content"

		progress := waveUploadBytes(t, contentURL, "admin", content)
		waveExpect(t, progress, http.StatusOK)
		var progressBody struct {
			BytesReceived int64 `json:"bytesReceived"`
		}
		waveDecode(t, progress, &progressBody)
		if progressBody.BytesReceived != int64(len(content)) {
			t.Fatalf("progress = %+v", progressBody)
		}
		if conflict := waveUploadBytes(t, contentURL+"?offset=1", "admin", content); conflict.Status != http.StatusConflict {
			t.Fatalf("offset conflict = %d body=%s", conflict.Status, conflict.Body)
		}

		complete := waveDo(t, base+"/api/v1/nodes/n1/files/uploads/"+session.UploadID+"/complete", http.MethodPost, nil, "admin")
		waveExpect(t, complete, http.StatusOK)
		var completed struct {
			Status string `json:"status"`
			Files  []struct {
				SizeBytes int64  `json:"sizeBytes"`
				Version   string `json:"version"`
			} `json:"files"`
		}
		waveDecode(t, complete, &completed)
		if completed.Status != "completed" || len(completed.Files) != 1 ||
			completed.Files[0].SizeBytes != int64(len(content)) || completed.Files[0].Version != digest {
			t.Fatalf("complete = %+v", completed)
		}
		pushed, pushedSize, pushedDigest, calls := env.upload.snapshot()
		if !bytes.Equal(pushed, content) || pushedSize != int64(len(content)) || pushedDigest != digest || calls != 1 {
			t.Fatalf("pushed = %q size=%d digest=%s calls=%d", pushed, pushedSize, pushedDigest, calls)
		}

		mismatchBytes := []byte("other")
		mismatch := waveDo(t, base+"/api/v1/nodes/n1/files/uploads", http.MethodPost, map[string]any{
			"destinationDir": "/srv/app",
			"files":          []map[string]any{{"name": "mismatch.bin", "sizeBytes": len(mismatchBytes), "sha256": waveDigest([]byte("expected"))}},
		}, "admin")
		waveExpect(t, mismatch, http.StatusCreated)
		var mismatchSession struct {
			UploadID string `json:"uploadId"`
		}
		waveDecode(t, mismatch, &mismatchSession)
		mismatchURL := base + "/api/v1/nodes/n1/files/uploads/" + mismatchSession.UploadID
		waveExpect(t, waveUploadBytes(t, mismatchURL+"/content", "admin", mismatchBytes), http.StatusOK)
		waveExpect(t, waveDo(t, mismatchURL+"/complete", http.MethodPost, nil, "admin"), http.StatusUnprocessableEntity)

		missing := waveDo(t, base+"/api/v1/nodes/n1/files/uploads", http.MethodPost, map[string]any{
			"destinationDir": "/srv/app",
			"files":          []map[string]any{{"name": "empty.bin", "sizeBytes": 3, "sha256": waveDigest([]byte("abc"))}},
		}, "admin")
		waveExpect(t, missing, http.StatusCreated)
		var missingSession struct {
			UploadID string `json:"uploadId"`
		}
		waveDecode(t, missing, &missingSession)
		waveExpect(t, waveDo(t, base+"/api/v1/nodes/n1/files/uploads/"+missingSession.UploadID+"/complete", http.MethodPost, nil, "admin"), http.StatusBadRequest)

		oversize := waveDo(t, base+"/api/v1/nodes/n1/files/uploads", http.MethodPost, map[string]any{
			"destinationDir": "/srv/app",
			"files":          []map[string]any{{"name": "oversize.bin", "sizeBytes": 2}},
		}, "admin")
		waveExpect(t, oversize, http.StatusCreated)
		var oversizeSession struct {
			UploadID string `json:"uploadId"`
		}
		waveDecode(t, oversize, &oversizeSession)
		if response := waveUploadBytes(t, base+"/api/v1/nodes/n1/files/uploads/"+oversizeSession.UploadID+"/content", "admin", []byte("abc")); response.Status != http.StatusRequestEntityTooLarge {
			t.Fatalf("oversize content = %d body=%s", response.Status, response.Body)
		}

		if denied := waveDo(t, base+"/api/v1/nodes/n2/files/uploads", http.MethodPost, map[string]any{
			"destinationDir": "/srv/app",
			"files":          []map[string]any{{"name": "denied.bin", "sizeBytes": 1}},
		}, "limited"); denied.Status != http.StatusForbidden {
			t.Fatalf("limited upload = %d body=%s", denied.Status, denied.Body)
		}
		if anonymous := waveDo(t, base+"/api/v1/nodes/n1/files/uploads", http.MethodPost, map[string]any{}, "anonymous"); anonymous.Status != http.StatusUnauthorized {
			t.Fatalf("anonymous upload = %d", anonymous.Status)
		}
	})

	t.Run("publication execution and rollback", func(t *testing.T) {
		draft := map[string]any{
			"serviceGroupId": "sg1", "gatewayIds": []string{"gw1"}, "domain": "wave4.example.com", "https": true,
		}
		created := waveDo(t, base+"/api/v1/publishing/publications", http.MethodPost, draft, "admin")
		waveExpect(t, created, http.StatusAccepted)
		var operation struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Stages []struct {
				Name   string `json:"name"`
				Status string `json:"status"`
			} `json:"stages"`
		}
		waveDecode(t, created, &operation)
		if operation.ID == "" || operation.Status != "queued" || len(operation.Stages) != 3 {
			t.Fatalf("created operation = %+v", operation)
		}

		run := waveDo(t, base+"/api/v1/publishing/publications/"+operation.ID+"/run", http.MethodPost, nil, "admin")
		waveExpect(t, run, http.StatusOK)
		waveDecode(t, run, &operation)
		if operation.Status != "succeeded" || len(operation.Stages) != 3 {
			t.Fatalf("run operation = %+v", operation)
		}
		for _, stage := range operation.Stages {
			if stage.Status != "succeeded" {
				t.Fatalf("stage = %+v", operation.Stages)
			}
		}
		order := env.callLog.snapshot()
		if len(order) != 3 || order[0] != publishing.StageDNS || order[1] != publishing.StageCertificate || order[2] != publishing.StageGateway {
			t.Fatalf("stage order = %v", order)
		}

		failing := waveDo(t, base+"/api/v1/publishing/publications", http.MethodPost, draft, "admin")
		waveExpect(t, failing, http.StatusAccepted)
		var failingPlan struct {
			ID string `json:"id"`
		}
		waveDecode(t, failing, &failingPlan)
		env.pubGateway.setFail(errors.New("provider refused"))
		failed := waveDo(t, base+"/api/v1/publishing/publications/"+failingPlan.ID+"/run", http.MethodPost, nil, "admin")
		waveExpect(t, failed, http.StatusBadGateway)
		var failure struct {
			Status string `json:"status"`
			Stages []struct {
				Name   string `json:"name"`
				Status string `json:"status"`
			} `json:"stages"`
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		waveDecode(t, failed, &failure)
		state := map[string]string{}
		for _, stage := range failure.Stages {
			state[stage.Name] = stage.Status
		}
		if failure.Error.Code != "stage_failed" || failure.Status != "partially_completed" ||
			state["dns"] != "succeeded" || state["certificate"] != "succeeded" || state["gateway"] != "failed" {
			t.Fatalf("failure = %+v", failure)
		}
		if bytes.Contains(failed.Body, []byte("provider refused")) {
			t.Fatalf("provider error leaked: %s", failed.Body)
		}

		env.pubGateway.setFail(nil)
		if retry := waveDo(t, base+"/api/v1/publishing/publications/"+failingPlan.ID+"/run", http.MethodPost, nil, "admin"); retry.Status != http.StatusOK {
			t.Fatalf("retry = %d body=%s", retry.Status, retry.Body)
		}
		dnsPublishes, _ := env.pubDNS.counts()
		certPublishes, _ := env.pubCert.counts()
		gwPublishes, _ := env.pubGateway.counts()
		if dnsPublishes != 2 || certPublishes != 2 || gwPublishes != 3 {
			t.Fatalf("publish counts = dns:%d cert:%d gateway:%d", dnsPublishes, certPublishes, gwPublishes)
		}

		rollbackDNS := waveDo(t, base+"/api/v1/publishing/publications/"+failingPlan.ID+"/rollback", http.MethodPost, map[string]any{"stage": "dns"}, "admin")
		waveExpect(t, rollbackDNS, http.StatusOK)
		waveDecode(t, rollbackDNS, &operation)
		if operation.Stages[0].Name != "dns" || operation.Stages[0].Status != "rolled_back" {
			t.Fatalf("rollback operation = %+v", operation)
		}
		for _, stage := range []string{"certificate", "gateway"} {
			response := waveDo(t, base+"/api/v1/publishing/publications/"+failingPlan.ID+"/rollback", http.MethodPost, map[string]any{"stage": stage}, "admin")
			waveExpect(t, response, http.StatusOK)
		}
		after := waveDo(t, base+"/api/v1/publishing/publications/"+failingPlan.ID, http.MethodGet, nil, "admin")
		waveExpect(t, after, http.StatusOK)
		waveDecode(t, after, &operation)
		for _, stage := range operation.Stages {
			if stage.Status != "rolled_back" {
				t.Fatalf("stage after rollback = %+v", operation.Stages)
			}
		}
		if _, dnsRollbacks := env.pubDNS.counts(); dnsRollbacks != 1 {
			t.Fatalf("dns rollbacks = %d", dnsRollbacks)
		}

		pending := waveDo(t, base+"/api/v1/publishing/publications", http.MethodPost, draft, "admin")
		waveExpect(t, pending, http.StatusAccepted)
		var pendingPlan struct {
			ID string `json:"id"`
		}
		waveDecode(t, pending, &pendingPlan)
		if response := waveDo(t, base+"/api/v1/publications/"+pendingPlan.ID+"/rollback", http.MethodPost, map[string]any{"stage": "dns"}, "admin"); response.Status != http.StatusBadRequest {
			t.Fatalf("pending rollback = %d body=%s", response.Status, response.Body)
		}
		if denied := waveDo(t, base+"/api/v1/publishing/publications/"+pendingPlan.ID+"/run", http.MethodPost, nil, "intruder"); denied.Status != http.StatusForbidden {
			t.Fatalf("intruder run = %d body=%s", denied.Status, denied.Body)
		}
		if anonymous := waveDo(t, base+"/api/v1/publishing/publications/"+pendingPlan.ID+"/run", http.MethodPost, nil, "anonymous"); anonymous.Status != http.StatusUnauthorized {
			t.Fatalf("anonymous run = %d", anonymous.Status)
		}
		if missing := waveDo(t, base+"/api/v1/publishing/publications/missing/run", http.MethodPost, nil, "admin"); missing.Status != http.StatusNotFound {
			t.Fatalf("missing run = %d", missing.Status)
		}
	})

	t.Run("audit", func(t *testing.T) {
		for _, action := range []string{
			monitoring.ActionCheck, files.ActionUploadCreate, files.ActionUploadComplete,
			publishing.ActionCreate, publishing.ActionRun, publishing.ActionRollback,
		} {
			page, err := env.recorder.Query(context.Background(), audit.Query{Action: action, Limit: 50})
			if err != nil {
				t.Fatalf("audit query %s: %v", action, err)
			}
			if page.Total == 0 {
				t.Fatalf("audit action %s has no events", action)
			}
		}
	})
}

// TestWave5RealProberAndLocalStagingTransport wires the real stdlib NetProber
// and the real local staging transport into the HTTP stack. Probes reach a local
// httptest server and uploads land in a private temporary directory; nothing is
// stubbed.
func TestWave5RealProberAndLocalStagingTransport(t *testing.T) {
	const probeBody = "wave5-probe-response-body"
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(probeBody))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("missing-path-secret"))
	}))
	defer backend.Close()
	backendHost, backendPort, err := net.SplitHostPort(strings.TrimPrefix(backend.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}

	prober, err := monitoring.NewNetProber(monitoring.NetProberConfig{AllowedHosts: []string{backendHost}})
	if err != nil {
		t.Fatal(err)
	}
	stagingRoot := filepath.Join(t.TempDir(), "node-staging")
	transport, err := files.NewLocalStagingTransport(stagingRoot)
	if err != nil {
		t.Fatal(err)
	}
	env := newWaveEnvWith(t, waveEnvOptions{
		Prober: prober, AllowedHosts: []string{backendHost},
		Uploads: transport, Downloads: transport,
		StagingDir: filepath.Join(t.TempDir(), "staging"),
	})
	base := env.server.URL

	t.Run("real http probe persists history", func(t *testing.T) {
		created := waveDo(t, base+"/api/v1/monitoring/availability-monitors", http.MethodPost, map[string]any{
			"name": "Wave 5 real probe", "type": "http",
			"target":          "http://" + backendHost + ":" + backendPort + "/health",
			"intervalSeconds": 60, "timeoutSeconds": 5,
		}, "admin")
		waveExpect(t, created, http.StatusCreated)
		var monitor struct {
			ID string `json:"id"`
		}
		waveDecode(t, created, &monitor)

		probe := waveDo(t, base+"/api/v1/monitoring/availability-monitors/"+monitor.ID+"/probe", http.MethodPost, nil, "admin")
		waveExpect(t, probe, http.StatusOK)
		if bytes.Contains(probe.Body, []byte(probeBody)) {
			t.Fatalf("probe response echoed the target body: %s", probe.Body)
		}
		var probeBodyDTO struct {
			MonitorID  string   `json:"monitorId"`
			Status     string   `json:"status"`
			State      string   `json:"state"`
			StatusCode *int     `json:"statusCode"`
			LatencyMs  *float64 `json:"latencyMs"`
			Error      string   `json:"error"`
			CheckedAt  string   `json:"checkedAt"`
		}
		waveDecode(t, probe, &probeBodyDTO)
		if probeBodyDTO.MonitorID != monitor.ID || probeBodyDTO.Status != "up" || probeBodyDTO.State != "up" ||
			probeBodyDTO.StatusCode == nil || *probeBodyDTO.StatusCode != http.StatusOK ||
			probeBodyDTO.LatencyMs == nil || *probeBodyDTO.LatencyMs < 0 ||
			probeBodyDTO.Error != "" || probeBodyDTO.CheckedAt == "" {
			t.Fatalf("probe = %+v", probeBodyDTO)
		}

		history := waveDo(t, base+"/api/v1/availability-monitors/"+monitor.ID+"/history?limit=5", http.MethodGet, nil, "admin")
		waveExpect(t, history, http.StatusOK)
		var page struct {
			Items []struct {
				Status     string   `json:"status"`
				StatusCode *int     `json:"statusCode"`
				LatencyMs  *float64 `json:"latencyMs"`
				Error      string   `json:"error"`
			} `json:"items"`
		}
		waveDecode(t, history, &page)
		if len(page.Items) != 1 || page.Items[0].Status != "up" ||
			page.Items[0].StatusCode == nil || *page.Items[0].StatusCode != http.StatusOK ||
			page.Items[0].LatencyMs == nil || page.Items[0].Error != "" {
			t.Fatalf("history = %+v", page)
		}

		failing := waveDo(t, base+"/api/v1/monitoring/availability-monitors", http.MethodPost, map[string]any{
			"name": "Wave 5 failing probe", "type": "http",
			"target":          "http://" + backendHost + ":" + backendPort + "/missing",
			"intervalSeconds": 60, "timeoutSeconds": 5,
		}, "admin")
		waveExpect(t, failing, http.StatusCreated)
		var failingMonitor struct {
			ID string `json:"id"`
		}
		waveDecode(t, failing, &failingMonitor)
		failed := waveDo(t, base+"/api/v1/monitoring/availability-monitors/"+failingMonitor.ID+"/probe", http.MethodPost, nil, "admin")
		waveExpect(t, failed, http.StatusOK)
		var failedBody struct {
			Status     string `json:"status"`
			StatusCode *int   `json:"statusCode"`
			Error      string `json:"error"`
		}
		waveDecode(t, failed, &failedBody)
		if failedBody.Status != "down" || failedBody.StatusCode == nil ||
			*failedBody.StatusCode != http.StatusNotFound || failedBody.Error != "http_status_404" {
			t.Fatalf("failed probe = %+v body=%s", failedBody, failed.Body)
		}
		if bytes.Contains(failed.Body, []byte("missing-path-secret")) {
			t.Fatalf("failed probe echoed the target body: %s", failed.Body)
		}
	})

	t.Run("real local staging upload and download", func(t *testing.T) {
		content := []byte("wave5-real-staging-transport-content")
		digest := waveDigest(content)
		created := waveDo(t, base+"/api/v1/nodes/n1/files/uploads", http.MethodPost, map[string]any{
			"destinationDir": "/srv/app",
			"files":          []map[string]any{{"name": "wave5.bin", "sizeBytes": len(content), "sha256": digest}},
		}, "admin")
		waveExpect(t, created, http.StatusCreated)
		var session struct {
			UploadID string `json:"uploadId"`
		}
		waveDecode(t, created, &session)

		progress := waveUploadBytes(t, base+"/api/v1/nodes/n1/files/uploads/"+session.UploadID+"/content", "admin", content)
		waveExpect(t, progress, http.StatusOK)
		var progressBody struct {
			BytesReceived int64 `json:"bytesReceived"`
		}
		waveDecode(t, progress, &progressBody)
		if progressBody.BytesReceived != int64(len(content)) {
			t.Fatalf("progress = %+v", progressBody)
		}

		complete := waveDo(t, base+"/api/v1/nodes/n1/files/uploads/"+session.UploadID+"/complete", http.MethodPost, nil, "admin")
		waveExpect(t, complete, http.StatusOK)
		var completed struct {
			Status string `json:"status"`
			Files  []struct {
				SizeBytes int64  `json:"sizeBytes"`
				Version   string `json:"version"`
			} `json:"files"`
		}
		waveDecode(t, complete, &completed)
		if completed.Status != "completed" || len(completed.Files) != 1 ||
			completed.Files[0].SizeBytes != int64(len(content)) || completed.Files[0].Version != digest {
			t.Fatalf("complete = %+v", completed)
		}

		onDisk, err := os.ReadFile(filepath.Join(stagingRoot, "n1", "srv", "app", "wave5.bin"))
		if err != nil {
			t.Fatalf("read staged file: %v", err)
		}
		if !bytes.Equal(onDisk, content) {
			t.Fatalf("staged file = %q", onDisk)
		}

		var downloaded bytes.Buffer
		written, err := env.fileSvc.Download(context.Background(), "admin", "req-wave5", "n1", "/srv/app/wave5.bin", &downloaded)
		if err != nil {
			t.Fatalf("download: %v", err)
		}
		if written != int64(len(content)) || !bytes.Equal(downloaded.Bytes(), content) {
			t.Fatalf("downloaded written=%d content=%q", written, downloaded.Bytes())
		}

		if _, err := env.fileSvc.Download(context.Background(), "admin", "req-wave5b", "n1", "/srv/app/never-staged.bin", &bytes.Buffer{}); err == nil {
			t.Fatal("download of a file that was never staged succeeded")
		}
	})
}
