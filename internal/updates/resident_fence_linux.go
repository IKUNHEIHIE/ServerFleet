package updates

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

func newResidentFenceControl(root *os.Root, c ResidentSupervisorConfig, process *OwnedProcessSupervisor, mu *sync.Mutex) ResidentFenceControl {
	current := func(ctx context.Context, request ResidentFenceRequest) (ResidentFenceRecord, error) {
		var record ResidentFenceRecord
		if ctx == nil || ctx.Err() != nil || request.OldAuthorityID != c.NodeID || !validPlanDigest(request.AuthorizationSHA256) || !installID(request.NewAuthorityID) || request.NewAuthorityID == c.NodeID || request.TrustGeneration == 0 {
			return record, ErrArtifactRejected
		}
		raw, e := readSizedRegularFile(filepath.Join(c.Directory, "fenced.json"), 8192)
		if e != nil || json.Unmarshal(raw, &record) != nil || record.Request != request || record.SupervisorPolicySHA256 != ResidentPolicySHA(c) || !validPlanDigest(record.ImageSHA256) || record.ConfirmedAt.IsZero() || record.ConfirmedAt.After(time.Now()) {
			return ResidentFenceRecord{}, ErrApplyUnknown
		}
		for _, name := range []string{"supervisor-reservation.json", "update-lock.json"} {
			if _, e := root.Lstat(name); !errors.Is(e, os.ErrNotExist) {
				return ResidentFenceRecord{}, ErrApplyUnknown
			}
		}
		if process != nil && !process.StoppedOwned() {
			return ResidentFenceRecord{}, ErrApplyUnknown
		}
		return record, nil
	}
	return ResidentFenceControl{
		Current: func(ctx context.Context, r ResidentFenceRequest) (ResidentFenceRecord, error) {
			if !mu.TryLock() {
				return ResidentFenceRecord{}, ErrUpdateBusy
			}
			defer mu.Unlock()
			return current(ctx, r)
		},
		Fence: func(ctx context.Context, r ResidentFenceRequest) (ResidentFenceRecord, error) {
			if !mu.TryLock() {
				return ResidentFenceRecord{}, ErrUpdateBusy
			}
			defer mu.Unlock()
			if result, e := current(ctx, r); e == nil {
				return result, nil
			}
			if c.Component != "controller" || r.OldAuthorityID != c.NodeID || !validPlanDigest(r.AuthorizationSHA256) || !installID(r.NewAuthorityID) || r.NewAuthorityID == c.NodeID || r.TrustGeneration == 0 || process == nil {
				return ResidentFenceRecord{}, ErrArtifactRejected
			}
			if _, e := root.Lstat("update-lock.json"); !errors.Is(e, os.ErrNotExist) {
				return ResidentFenceRecord{}, ErrUpdateBusy
			}
			if writeResidentRecord(root, "fenced-intent.json", r) != nil {
				return ResidentFenceRecord{}, ErrApplyUnknown
			}
			image, e := process.FenceOwned(ctx)
			if e != nil {
				return ResidentFenceRecord{}, ErrApplyUnknown
			}
			record := ResidentFenceRecord{Request: r, SupervisorPolicySHA256: ResidentPolicySHA(c), ImageSHA256: image.SHA256, ConfirmedAt: time.Now().UTC()}
			if writeResidentRecord(root, "fenced.json", record) != nil {
				return ResidentFenceRecord{}, ErrApplyUnknown
			}
			return current(ctx, r)
		},
	}
}
