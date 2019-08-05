package blob

import (
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/pkg/errors"

	"github.com/buildpack/pack/internal/archive"
)

type Buildpack struct {
	Info   BuildpackInfo `toml:"buildpack"`
	Stacks []Stack       `toml:"stacks"`
	Order  Order         `toml:"order"`
	Blob   `toml:"-"`
}

type BuildpackInfo struct {
	ID      string `toml:"id" json:"id"`
	Version string `toml:"version" json:"version"`
}

type Order []Group

type Group struct {
	Group []BuildpackInfo
}

type Stack struct {
	ID string
}

func NewBuildpack(path string) (Buildpack, error) {
	bp := Buildpack{Blob: Blob{Path: path}}
	rc, err := bp.Open()
	if err != nil {
		return Buildpack{}, errors.Wrap(err, "open buildpack")
	}
	defer rc.Close()
	_, buf, err := archive.ReadTarEntry(rc, "buildpack.toml")
	_, err = toml.Decode(string(buf), &bp)
	if err != nil {
		return Buildpack{}, errors.Wrapf(err, "reading buildpack.toml from path %s", path)
	}
	return bp, nil
}

func (b *Buildpack) EscapedID() string {
	return strings.Replace(b.Info.ID, "/", "_", -1)
}

func (b *Buildpack) SupportsStack(stackID string) bool {
	for _, stack := range b.Stacks {
		if stack.ID == stackID {
			return true
		}
	}
	return false
}
