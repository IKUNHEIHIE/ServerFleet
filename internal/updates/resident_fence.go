package updates

import (
	"context"
	"time"
)

type ResidentFenceRequest struct {
	AuthorizationSHA256 string `json:"authorizationSha256"`
	OldAuthorityID      string `json:"oldAuthorityId"`
	NewAuthorityID      string `json:"newAuthorityId"`
	TrustGeneration     uint64 `json:"trustGeneration"`
}
type ResidentFenceRecord struct {
	Request                ResidentFenceRequest `json:"request"`
	SupervisorPolicySHA256 string               `json:"supervisorPolicySha256"`
	ImageSHA256            string               `json:"imageSha256"`
	ConfirmedAt            time.Time            `json:"confirmedAt"`
}
type ResidentFenceControl struct {
	Fence   func(context.Context, ResidentFenceRequest) (ResidentFenceRecord, error)
	Current func(context.Context, ResidentFenceRequest) (ResidentFenceRecord, error)
}
