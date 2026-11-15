package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/files"
)

// agentDirectoryTransport adapts the Controller's agent FileClient to the files
// service's protobuf-free DirectoryTransport. The service has already authorized
// the subject for the node and operation; here we only correlate the ad-hoc
// request by request_id and translate the agent's result code. The FileClient is
// read lazily because agent transport is composed after this point.
type agentDirectoryTransport struct {
	server *Server
}

func agentDirectoryOps(s *Server) files.DirectoryTransport {
	return agentDirectoryTransport{server: s}
}

func newFileOpRequestID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "dirop-" + hex.EncodeToString(b), nil
}

func (a agentDirectoryTransport) DirectoryOp(ctx context.Context, nodeID string, op files.DirectoryOp) (files.DirectoryResult, error) {
	client := a.server.AgentFiles
	if client == nil {
		return files.DirectoryResult{}, files.ErrMissingDirectoryTransport
	}
	requestID, err := newFileOpRequestID()
	if err != nil {
		return files.DirectoryResult{}, err
	}
	reply, err := client.DirectoryOp(ctx, nodeID, &agentv1.FileOpRequest{
		RequestId: requestID, NodeId: nodeID, Op: op.Op, Path: op.Path, NewName: op.NewName, Recursive: op.Recursive,
	})
	if err != nil {
		return files.DirectoryResult{}, err
	}
	result := files.DirectoryResult{Code: reply.ResultCode, Path: reply.Path, Size: int64(reply.Size)}
	for _, e := range reply.Entries {
		result.Entries = append(result.Entries, files.DirectoryEntry{Name: e.Name, Type: e.Type, Size: int64(e.Size), ModifiedUnixMs: e.ModifiedUnixMs})
	}
	switch reply.ResultCode {
	case "ok":
		return result, nil
	case "not_found":
		return result, files.ErrDirectoryNotFound
	case "unauthorized":
		return result, auth.ErrDenied
	default:
		return result, files.ErrDirectoryOp
	}
}
