package services_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/certificates"
	"serverfleet.local/serverfleet/internal/dns"
	"serverfleet.local/serverfleet/internal/files"
	"serverfleet.local/serverfleet/internal/gateway"
	"serverfleet.local/serverfleet/internal/monitoring"
	"serverfleet.local/serverfleet/internal/nodes"
	"serverfleet.local/serverfleet/internal/notifications"
	"serverfleet.local/serverfleet/internal/publishing"
	"serverfleet.local/serverfleet/internal/quota"
	"serverfleet.local/serverfleet/internal/servicegroups"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/storage/testkit"
	"serverfleet.local/serverfleet/internal/tasks"
)

func v09Migrations() [][]storage.Migration {
	return [][]storage.Migration{
		nodes.Migrations(), servicegroups.Migrations(), tasks.Migrations(),
		files.Migrations(), monitoring.Migrations(), notifications.Migrations(),
		quota.Migrations(), dns.Migrations(), certificates.Migrations(),
		gateway.Migrations(), publishing.Migrations(), audit.Migrations(),
	}
}

func TestBackupRestoreAcrossDomains(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	livePath := filepath.Join(dir, "live.db")
	db := testkit.NewDBAt(t, livePath, v09Migrations()...)

	nodeRepo, _ := nodes.NewRepo(db)
	groupRepo, _ := nodes.NewGroupRepo(db)
	serviceRepo, err := servicegroups.NewRepo(db, nodeRepo)
	if err != nil {
		t.Fatal(err)
	}
	taskRepo, _ := tasks.NewRepo(db)
	fileRepo, _ := files.NewRepo(db)

	node, err := nodeRepo.Create(ctx, db, nodes.NodeCreateInput{ID: "n1", Name: "Node One", Region: "hkg"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, applied, err := nodeRepo.ApplyAgentUpdate(ctx, db, nodes.AgentUpdate{
		NodeID: "n1", Status: nodes.StatusOnline, Sequence: 1, ObservedAt: now,
		Metrics: &nodes.Metrics{CPUPercent: 12, MemoryPercent: 34}, SampleSource: nodes.SampleSourceAgent,
	}); err != nil || !applied {
		t.Fatalf("agent update: %v applied=%t", err, applied)
	}
	if _, err := groupRepo.Create(ctx, db, "grp-1", "Maintenance", "", []string{"n1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := serviceRepo.Create(ctx, db, servicegroups.Input{
		ID: "sg-1", Name: "Web", MemberNodeIDs: []string{"n1"},
		Service:       servicegroups.ServiceConfig{Port: 8080, Protocol: "http"},
		LoadBalancing: servicegroups.LoadBalancing{Enabled: true, Strategy: "round_robin"},
	}); err != nil {
		t.Fatal(err)
	}
	task := tasks.Task{
		ID: "tsk-1", Name: "patch", SubmitterID: "user-1", ActionID: "host.patch",
		OperationClass: tasks.OperationClassStandard, Params: "{}", Revision: 1,
		Status: tasks.StatusQueued, PlanHash: "digest", TargetType: "node_ids",
		UnstartedDeadline: now.Add(15 * time.Minute), RequestID: "req-1",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := taskRepo.Create(ctx, db, task, []tasks.Target{{NodeID: "n1", Position: 0, State: tasks.TargetPending}}); err != nil {
		t.Fatal(err)
	}
	if err := fileRepo.CreateTransfer(ctx, db, files.Transfer{
		ID: "xfer-1", TaskID: "tsk-1", SubmitterID: "user-1", SourceNodeID: "n1",
		DestinationNodeID: "n1", DestinationDir: "/srv", ConflictPolicy: files.ConflictSkip,
		Status: files.TransferQueued, RequestID: "req-1", CreatedAt: now, UpdatedAt: now,
		Items: []files.TransferItem{{Sequence: 0, SourcePath: "/etc/a", Status: files.ItemQueued}},
	}); err != nil {
		t.Fatal(err)
	}
	limits := audit.DefaultLimits()
	limits.RequireSource = true
	validator, err := audit.NewValidator([]string{"nodes.update"}, limits)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := audit.NewSQLRecorder(db, validator, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.Append(ctx, audit.Event{
		SubjectID: "user-1", Action: "nodes.update", Result: "success", RequestID: "req-1",
		Source: audit.SourceWeb, NodeIDs: []string{"n1"}, At: now,
	}); err != nil {
		t.Fatal(err)
	}

	snapshot := filepath.Join(dir, "snapshot.db")
	if err := db.BackupTo(ctx, snapshot); err != nil {
		t.Fatalf("backup: %v", err)
	}
	// A write after the snapshot must not appear in the restored copy.
	if err := recorder.Append(ctx, audit.Event{
		SubjectID: "user-1", Action: "nodes.update", Result: "success", RequestID: "req-2",
		Source: audit.SourceWeb, NodeIDs: []string{"n1"}, At: now.Add(time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	restoredPath := filepath.Join(dir, "restored.db")
	if err := storage.RestoreFile(ctx, snapshot, restoredPath); err != nil {
		t.Fatalf("restore: %v", err)
	}
	restored, err := storage.Open(ctx, storage.Config{Path: restoredPath})
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	migrator, err := storage.NewMigrator(v09Migrations()...)
	if err != nil {
		t.Fatal(err)
	}
	report, err := migrator.Apply(ctx, restored)
	if err != nil || len(report.Applied) != 0 {
		t.Fatalf("reapply migrations: %+v %v", report, err)
	}
	versions, err := storage.AppliedVersions(ctx, restored)
	if err != nil || len(versions) != 14 {
		t.Fatalf("versions = %v, %v", versions, err)
	}

	restoredNodes, _ := nodes.NewRepo(restored)
	got, err := restoredNodes.Get(ctx, node.ID)
	if err != nil || got.Name != "Node One" || got.Status != nodes.StatusOnline || got.Metrics == nil {
		t.Fatalf("restored node = %+v, %v", got, err)
	}
	restoredGroups, _ := nodes.NewGroupRepo(restored)
	group, err := restoredGroups.Get(ctx, "grp-1")
	if err != nil || len(group.Members) != 1 {
		t.Fatalf("restored group = %+v, %v", group, err)
	}
	restoredServices, _ := servicegroups.NewRepo(restored, restoredNodes)
	service, err := restoredServices.Get(ctx, "sg-1")
	if err != nil || service.Service.Port != 8080 {
		t.Fatalf("restored service group = %+v, %v", service, err)
	}
	restoredTasks, _ := tasks.NewRepo(restored)
	restoredTask, err := restoredTasks.Get(ctx, "tsk-1")
	if err != nil || len(restoredTask.Targets) != 1 {
		t.Fatalf("restored task = %+v, %v", restoredTask, err)
	}
	restoredFiles, _ := files.NewRepo(restored)
	transfer, err := restoredFiles.GetTransfer(ctx, "xfer-1")
	if err != nil || len(transfer.Items) != 1 {
		t.Fatalf("restored transfer = %+v, %v", transfer, err)
	}
	restoredRecorder, err := audit.NewSQLRecorder(restored, validator, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	count, err := restoredRecorder.Count(ctx)
	if err != nil || count != 1 {
		t.Fatalf("restored audit count = %d, %v (post-backup write leaked)", count, err)
	}
}

func TestRestoreRejectsCorruptSnapshot(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db := testkit.NewDBAt(t, filepath.Join(dir, "live.db"), v09Migrations()...)
	snapshot := filepath.Join(dir, "snapshot.db")
	if err := db.BackupTo(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	corrupt := filepath.Join(dir, "corrupt.db")
	if err := os.WriteFile(corrupt, body[:64], 0o600); err != nil {
		t.Fatal(err)
	}
	if err := storage.RestoreFile(ctx, corrupt, filepath.Join(dir, "restored.db")); err == nil {
		t.Fatal("corrupt snapshot was accepted")
	}
}
