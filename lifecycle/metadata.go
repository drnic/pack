package lifecycle

import (
	"github.com/Masterminds/semver"
)

type Lifecycle struct {
	Version *semver.Version `json:"version"`
	Path    string          `json:"-"`
}
