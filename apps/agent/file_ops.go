package main

import (
	"context"
	"io/fs"
	"os"
	"path"
	"strings"

	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"serverfleet.local/serverfleet/internal/transport"
)

// HandleOp performs an ad-hoc directory operation strictly under the private
// file root. Authorization happens at the Controller before the frame reaches
// the agent; here every path is confined by safePath + os.Root, so no escape
// (symlink or "..") is possible and no raw file bytes cross except listings.
func (f *agentFileSessions) HandleOp(ctx context.Context, envelope *agentv1.Envelope) (*agentv1.Envelope, error) {
	req := envelope.GetFileOpRequest()
	deny := func(code string) *agentv1.Envelope {
		return &agentv1.Envelope{Message: &agentv1.Envelope_FileOpReply{FileOpReply: &agentv1.FileOpReply{RequestId: req.GetRequestId(), ResultCode: code}}}
	}
	if req == nil || !transport.FileID(req.GetRequestId()) || req.GetNodeId() != f.node || !transport.ValidFileOp(req.GetOp()) || req.GetPath() == "" || ctx.Err() != nil {
		return deny("invalid"), nil
	}
	f.io.mu.Lock()
	defer f.io.mu.Unlock()
	p := strings.TrimPrefix(req.GetPath(), "/")
	if p == "" {
		p = "."
	}
	reply := &agentv1.FileOpReply{RequestId: req.GetRequestId(), Path: p}
	switch req.GetOp() {
	case "list":
		if f.io.safePath(p, false) != nil {
			return deny("not_found"), nil
		}
		entries, err := fs.ReadDir(f.io.root.FS(), p)
		if err != nil {
			return deny(fileOpCode(err)), nil
		}
		if len(entries) > transport.MaxFileOpEntries {
			entries = entries[:transport.MaxFileOpEntries]
		}
		for _, e := range entries {
			typ := "file"
			if e.IsDir() {
				typ = "directory"
			} else if e.Type()&fs.ModeSymlink != 0 {
				typ = "symlink"
			}
			var size uint64
			var mtime int64
			if info, ierr := e.Info(); ierr == nil {
				size = uint64(info.Size())
				mtime = info.ModTime().UnixMilli()
			}
			reply.Entries = append(reply.Entries, &agentv1.FileEntry{Name: e.Name(), Type: typ, Size: size, ModifiedUnixMs: mtime})
		}
		reply.ResultCode = "ok"
	case "stat":
		if f.io.safePath(p, false) != nil {
			return deny("not_found"), nil
		}
		info, err := f.io.root.Stat(p)
		if err != nil {
			return deny(fileOpCode(err)), nil
		}
		reply.Size = uint64(info.Size())
		reply.ResultCode = "ok"
	case "mkdir":
		if f.io.safePath(p, true) != nil {
			return deny("invalid"), nil
		}
		if err := f.io.root.Mkdir(p, 0o700); err != nil {
			return deny(fileOpCode(err)), nil
		}
		reply.ResultCode = "ok"
	case "rename":
		name := req.GetNewName()
		if !transport.ValidFileOpName(name) || f.io.safePath(p, false) != nil {
			return deny("invalid"), nil
		}
		target := path.Join(path.Dir(p), name)
		if err := f.io.root.Rename(p, target); err != nil {
			return deny(fileOpCode(err)), nil
		}
		reply.Path = target
		reply.ResultCode = "ok"
	case "remove":
		if f.io.safePath(p, false) != nil {
			return deny("not_found"), nil
		}
		var err error
		if req.GetRecursive() {
			err = f.io.root.RemoveAll(p)
		} else {
			err = f.io.root.Remove(p)
		}
		if err != nil {
			return deny(fileOpCode(err)), nil
		}
		reply.ResultCode = "ok"
	}
	return &agentv1.Envelope{Message: &agentv1.Envelope_FileOpReply{FileOpReply: reply}}, nil
}

func fileOpCode(err error) string {
	switch {
	case os.IsNotExist(err):
		return "not_found"
	case os.IsExist(err):
		return "conflict"
	default:
		return "io_error"
	}
}
