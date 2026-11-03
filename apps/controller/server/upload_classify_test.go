package server

import (
	"errors"
	"testing"

	"serverfleet.local/serverfleet/internal/files"
	"serverfleet.local/serverfleet/internal/tasks"
)

func TestClassifyUploadFailureTable(t *testing.T) {
	infra := errors.New("query timed out")
	// TaskBindingState maps an empty-row query result to a successful query
	// with an empty state: positive absence of any binding.
	cases := []struct {
		name    string
		ev      uploadEvidence
		code    tasks.NodeResultCode
		status  string
		wantErr string
	}{
		{
			name: "binding absent by positive query plus cancel request (empty state from TaskBindingState)",
			ev:   uploadEvidence{State: "", CancelRequested: true},
			code: tasks.NodeResultCanceled, status: files.UploadCancelled, wantErr: "cancelled_before_write",
		},
		{
			name: "binding reserved plus cancel request",
			ev:   uploadEvidence{State: "reserved", CancelRequested: true},
			code: tasks.NodeResultCanceled, status: files.UploadCancelled, wantErr: "cancelled_before_write",
		},
		{
			name: "binding accepted plus checksum rejection",
			ev:   uploadEvidence{State: "accepted", ChecksumRejected: true},
			code: tasks.NodeResultFailed, status: files.UploadFailed, wantErr: "denied_before_write",
		},
		{
			name: "binding absent plus positive denial",
			ev:   uploadEvidence{State: "", DeniedNow: true},
			code: tasks.NodeResultFailed, status: files.UploadFailed, wantErr: "denied_before_write",
		},
		{
			name: "binding query failure keeps unknown despite cancel",
			ev:   uploadEvidence{StateQueryErr: infra, CancelRequested: true},
			code: tasks.NodeResultUnknown, status: files.UploadFailed, wantErr: files.UploadErrorAgentUnknown,
		},
		{
			name: "binding query failure keeps unknown despite denial",
			ev:   uploadEvidence{StateQueryErr: infra, DeniedNow: true, ChecksumRejected: true},
			code: tasks.NodeResultUnknown, status: files.UploadFailed, wantErr: files.UploadErrorAgentUnknown,
		},
		{
			name: "opened binding keeps unknown even with cancel request",
			ev:   uploadEvidence{State: "opened", CancelRequested: true},
			code: tasks.NodeResultUnknown, status: files.UploadFailed, wantErr: files.UploadErrorAgentUnknown,
		},
		{
			name: "done binding keeps unknown",
			ev:   uploadEvidence{State: "done"},
			code: tasks.NodeResultUnknown, status: files.UploadFailed, wantErr: files.UploadErrorAgentUnknown,
		},
		{
			name: "task read failure with no binding keeps unknown",
			ev:   uploadEvidence{State: "", TaskReadErr: errors.New("task store unavailable")},
			code: tasks.NodeResultUnknown, status: files.UploadFailed, wantErr: files.UploadErrorAgentUnknown,
		},
		{
			name: "task read failure disables denial evidence too",
			ev:   uploadEvidence{State: "", TaskReadErr: errors.New("task store unavailable"), DeniedNow: true},
			code: tasks.NodeResultUnknown, status: files.UploadFailed, wantErr: files.UploadErrorAgentUnknown,
		},
	}
	for _, c := range cases {
		code, status, errorCode, message := classifyUploadFailure(c.ev)
		if code != c.code || status != c.status || errorCode != c.wantErr || message == "" {
			t.Fatalf("%s: code=%v status=%s errorCode=%q message=%q", c.name, code, status, errorCode, message)
		}
	}
}
