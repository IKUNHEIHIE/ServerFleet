package updates

import "time"

type OwnedProcessObservation struct {
	PID       int
	Nonce     string
	StartedAt time.Time
}
