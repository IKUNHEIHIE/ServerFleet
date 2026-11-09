package updates

import "strconv"

// Structural receiver check; actual signatures/image/PID/readiness must still
// be verified by the owning parent. Shape does not grant install authority.
func ValidInstallCommitReceipt(r InstallReceipt) bool {
	if !installID(r.Intent.TaskID) || !installID(r.Intent.ExecutionID) || !installedManifest(r.Intent.Previous) || !installedManifest(r.Attempted) || !installedManifest(r.Current) {
		return false
	}
	if r.Intent.Previous.Component != r.Attempted.Component || r.Intent.Previous.Architecture != r.Attempted.Architecture || r.Attempted.Sequence <= r.Intent.Previous.Sequence {
		return false
	}
	switch r.State {
	case "installed":
		if r.Current != r.Attempted {
			return false
		}
	case "rolled_back":
		if !r.Intent.AllowRollback || r.Current != r.Intent.Previous {
			return false
		}
	default:
		return false
	}
	h := r.Health
	pid, e := strconv.Atoi(h.ProcessID)
	return e == nil && pid > 0 && strconv.Itoa(pid) == h.ProcessID && h.ExecutionID == r.Intent.ExecutionID && h.Component == r.Current.Component && h.Version == r.Current.Version && h.SHA256 == r.Current.SHA256 && !h.StartedAt.IsZero() && !h.ObservedAt.Before(h.StartedAt)
}
