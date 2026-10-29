package services_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/nodes"
	"serverfleet.local/serverfleet/internal/storage/testkit"
	"serverfleet.local/serverfleet/internal/tasks"
)

type pocResult struct {
	Agents            int     `json:"agents"`
	HeartbeatsPerNode int     `json:"heartbeatsPerNode"`
	WriteDurationMS   int64   `json:"writeDurationMs"`
	WritesPerSecond   float64 `json:"writesPerSecond"`
	TaskEvents        int     `json:"taskEvents"`
	SSEEventsRead     int     `json:"sseEventsRead"`
	SSEReadDurationMS int64   `json:"sseReadDurationMs"`
	DatabaseBytes     int64   `json:"databaseBytes"`
	WALBytes          int64   `json:"walBytes"`
	HeapAllocBytes    uint64  `json:"heapAllocBytes"`
}

type pocReport struct {
	GeneratedAt string      `json:"generatedAt"`
	Environment string      `json:"environment"`
	Note        string      `json:"note"`
	Results     []pocResult `json:"results"`
}

// TestPOC07StorageTaskSSEThroughput simulates 10/30/100 agents writing node
// heartbeats, audit rows and task events against a real WAL database, then reads
// the task event stream with a cursor. This is dev-box evidence only: it does NOT
// substitute the approved 1 vCPU / 1GB Linux acceptance run.
func TestPOC07StorageTaskSSEThroughput(t *testing.T) {
	if testing.Short() {
		t.Skip("POC-07 is a throughput measurement")
	}
	const heartbeats = 20
	report := pocReport{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Environment: fmt.Sprintf("%s/%s/%d cpu", runtime.GOOS, runtime.GOARCH, runtime.NumCPU()),
		Note:        "Windows development evidence only; 1C1G Linux POC not executed",
	}
	for _, agents := range []int{10, 30, 100} {
		result := runPOCLoad(t, agents, heartbeats)
		report.Results = append(report.Results, result)
		t.Logf("agents=%d writes/s=%.1f db=%dKB wal=%dKB sse=%d/%dms heap=%dKB",
			result.Agents, result.WritesPerSecond, result.DatabaseBytes/1024,
			result.WALBytes/1024, result.SSEEventsRead, result.SSEReadDurationMS,
			result.HeapAllocBytes/1024)
	}
	writePOCReport(t, report)
}

func runPOCLoad(t *testing.T, agentCount, heartbeats int) pocResult {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	db := testkit.NewDBAt(t, filepath.Join(dir, "poc.db"), nodes.Migrations(), tasks.Migrations(), audit.Migrations())
	nodeRepo, _ := nodes.NewRepo(db)
	taskRepo, _ := tasks.NewRepo(db)
	limits := audit.DefaultLimits()
	limits.RequireSource = true
	validator, err := audit.NewValidator([]string{"nodes.heartbeat"}, limits)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := audit.NewSQLRecorder(db, validator, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, agentCount)
	for i := 0; i < agentCount; i++ {
		id := fmt.Sprintf("node-%03d", i)
		ids = append(ids, id)
		if _, err := nodeRepo.Create(ctx, db, nodes.NodeCreateInput{ID: id, Name: id}); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	start := time.Now()
	writes := 0
	for sequence := 1; sequence <= heartbeats; sequence++ {
		for _, id := range ids {
			if _, applied, err := nodeRepo.ApplyAgentUpdate(ctx, db, nodes.AgentUpdate{
				NodeID: id, Status: nodes.StatusOnline, Sequence: uint64(sequence),
				ObservedAt:   now.Add(time.Duration(sequence) * time.Second),
				Metrics:      &nodes.Metrics{CPUPercent: float64(sequence % 100), MemoryPercent: 40},
				SampleSource: nodes.SampleSourceAgent,
			}); err != nil || !applied {
				t.Fatalf("heartbeat: %v applied=%t", err, applied)
			}
			if err := recorder.Append(ctx, audit.Event{
				SubjectID: id, Action: "nodes.heartbeat", Result: "success",
				RequestID: fmt.Sprintf("hb-%s-%d", id, sequence), Source: audit.SourceAgent,
				NodeIDs: []string{id}, At: now.Add(time.Duration(sequence) * time.Second),
			}); err != nil {
				t.Fatal(err)
			}
			writes += 2
		}
	}
	writeDuration := time.Since(start)

	taskEvents := 0
	taskIDs := make([]string, 0, agentCount)
	for i, id := range ids {
		taskID := fmt.Sprintf("task-%03d", i)
		taskIDs = append(taskIDs, taskID)
		task := tasks.Task{
			ID: taskID, Name: "poc", SubmitterID: "poc", ActionID: "nodes.heartbeat",
			OperationClass: tasks.OperationClassStandard, Params: "{}", Revision: 1,
			Status: tasks.StatusQueued, PlanHash: "digest", TargetType: "node_ids",
			UnstartedDeadline: now.Add(time.Hour), RequestID: "poc", CreatedAt: now, UpdatedAt: now,
		}
		if err := taskRepo.Create(ctx, db, task, []tasks.Target{{NodeID: id, Position: 0, State: tasks.TargetPending}}); err != nil {
			t.Fatal(err)
		}
		for event := 0; event < 3; event++ {
			if err := taskRepo.AppendEvent(ctx, db, taskID, "poc", map[string]any{"sequence": event}, now); err != nil {
				t.Fatal(err)
			}
			taskEvents++
		}
	}

	readStart := time.Now()
	read := 0
	var cursor int64
	for {
		events, err := taskRepo.Events(ctx, taskIDs[0], cursor, 25)
		if err != nil {
			t.Fatal(err)
		}
		if len(events) == 0 {
			break
		}
		read += len(events)
		cursor = events[len(events)-1].Seq
	}
	readDuration := time.Since(readStart)

	if _, err := db.Exec(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		t.Fatal(err)
	}
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	dbInfo, err := os.Stat(db.Path())
	if err != nil {
		t.Fatal(err)
	}
	walBytes := int64(0)
	if info, err := os.Stat(db.Path() + "-wal"); err == nil {
		walBytes = info.Size()
	}
	result := pocResult{
		Agents: agentCount, HeartbeatsPerNode: heartbeats,
		WriteDurationMS: writeDuration.Milliseconds(),
		WritesPerSecond: float64(writes) / writeDuration.Seconds(),
		TaskEvents:      taskEvents, SSEEventsRead: read,
		SSEReadDurationMS: readDuration.Milliseconds(),
		DatabaseBytes:     dbInfo.Size(), WALBytes: walBytes, HeapAllocBytes: memory.HeapAlloc,
	}
	if result.SSEEventsRead != 3 {
		t.Fatalf("SSE read %d events, want 3", result.SSEEventsRead)
	}
	if result.WALBytes > 64<<20 {
		t.Fatalf("WAL grew to %d bytes", result.WALBytes)
	}
	return result
}

func writePOCReport(t *testing.T, report pocReport) {
	t.Helper()
	path := filepath.Join("..", "..", "..", "..", "reports", "_review", "v09-services", "poc07-report.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Logf("report directory: %v", err)
		return
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Logf("write report: %v", err)
	}
}
