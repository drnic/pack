package builder

import (
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/pkg/errors"

	"github.com/buildpack/pack/internal/archive"
)

type buildpack struct {
	descriptor BuildpackDescriptor
	Blob       `toml:"-"`
}

func (b *buildpack) Descriptor() BuildpackDescriptor {
	return b.descriptor
}

type BuildpackDescriptor struct {
	Info   BuildpackInfo `toml:"buildpack"`
	Stacks []Stack       `toml:"stacks"`
	Order  Order         `toml:"order"`
}

//go:generate mockgen -package testmocks -destination testmocks/buildpack.go github.com/buildpack/pack/builder BuildpackI
type Buildpack interface {
	Blob
	Descriptor() BuildpackDescriptor
}

type BuildpackInfo struct {
	ID      string `toml:"id" json:"id"`
	Version string `toml:"version" json:"version"`
}

type Stack struct {
	ID string
}

func NewBuildpack(blob Blob) (Buildpack, error) {
	bpd := BuildpackDescriptor{}
	rc, err := blob.Open()
	if err != nil {
		return nil, errors.Wrap(err, "open buildpack")
	}
	defer rc.Close()
	_, buf, err := archive.ReadTarEntry(rc, "buildpack.toml")
	_, err = toml.Decode(string(buf), &bpd)
	if err != nil {
		return nil, errors.Wrapf(err, "reading buildpack.toml")
	}
	return &buildpack{descriptor: bpd, Blob: blob}, nil
}

func (b *BuildpackDescriptor) EscapedID() string {
	return strings.Replace(b.Info.ID, "/", "_", -1)
}

func (b *BuildpackDescriptor) SupportsStack(stackID string) bool {
	for _, stack := range b.Stacks {
		if stack.ID == stackID {
			return true
		}
	}
	return false
}
