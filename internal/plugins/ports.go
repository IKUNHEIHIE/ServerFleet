// Package plugins owns optional package metadata and the future controlled host.
// No runtime or Xray business code is included in the core.
package plugins

import "context"

type Descriptor struct {
	ID      string
	Version string
	Digest  string
}

type Catalog interface {
	Lookup(context.Context, string) (Descriptor, error)
}
