// Package nodes owns node inventory and lifecycle, not scheduling or execution.
package nodes

import "context"

type Record struct {
	ID   string
	Name string
}

// Reader is an internal port, not a stable public API or a permission bypass.
type Reader interface {
	Get(context.Context, string) (Record, error)
}
