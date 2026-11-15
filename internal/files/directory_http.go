package files

import (
	"net/http"
	"time"
)

func entryPath(dir, name string) string {
	if dir == "" || dir == "." {
		return name
	}
	if dir == "/" {
		return "/" + name
	}
	return dir + "/" + name
}

func (api *httpAPI) listDirectory(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	nodeID := r.PathValue("nodeId")
	result, err := api.service.DirectoryOp(r.Context(), principal.SubjectID, principal.RequestID, nodeID, DirectoryOp{Op: "list", Path: r.URL.Query().Get("path")})
	if err != nil {
		writeError(w, err)
		return
	}
	entries := make([]map[string]any, 0, len(result.Entries))
	for _, e := range result.Entries {
		entry := map[string]any{"name": e.Name, "path": entryPath(result.Path, e.Name), "type": e.Type}
		if e.Type == "file" || e.Type == "symlink" {
			entry["sizeBytes"] = e.Size
		}
		if e.ModifiedUnixMs > 0 {
			entry["modifiedAt"] = time.UnixMilli(e.ModifiedUnixMs).UTC().Format(time.RFC3339Nano)
		}
		entries = append(entries, entry)
	}
	writeJSON(w, http.StatusOK, map[string]any{"nodeId": nodeID, "path": result.Path, "entries": entries, "nextCursor": nil})
}

func (api *httpAPI) removePath(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	result, err := api.service.DirectoryOp(r.Context(), principal.SubjectID, principal.RequestID, r.PathValue("nodeId"), DirectoryOp{Op: "remove", Path: query.Get("path"), Recursive: query.Get("recursive") == "true"})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": result.Path, "removed": true})
}

func (api *httpAPI) pathSize(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	var body struct {
		Path string `json:"path"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	result, err := api.service.DirectoryOp(r.Context(), principal.SubjectID, principal.RequestID, r.PathValue("nodeId"), DirectoryOp{Op: "stat", Path: body.Path})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": result.Path, "sizeBytes": result.Size})
}

func (api *httpAPI) createDirectory(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	var body struct {
		ParentPath string `json:"parentPath"`
		Name       string `json:"name"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	target, err := JoinRemotePath(body.ParentPath, body.Name)
	if err != nil {
		writeError(w, err)
		return
	}
	result, err := api.service.DirectoryOp(r.Context(), principal.SubjectID, principal.RequestID, r.PathValue("nodeId"), DirectoryOp{Op: "mkdir", Path: target})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"name": body.Name, "path": result.Path, "type": "directory"})
}

func (api *httpAPI) renamePath(w http.ResponseWriter, r *http.Request) {
	principal, ok := api.principal(w, r)
	if !ok {
		return
	}
	var body struct {
		Path    string `json:"path"`
		NewName string `json:"newName"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	result, err := api.service.DirectoryOp(r.Context(), principal.SubjectID, principal.RequestID, r.PathValue("nodeId"), DirectoryOp{Op: "rename", Path: body.Path, NewName: body.NewName})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": body.NewName, "path": result.Path})
}
