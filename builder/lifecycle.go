package builder

import (
	"archive/tar"
	"fmt"
	"io"
	"path"
	"regexp"

	"github.com/BurntSushi/toml"
	"github.com/Masterminds/semver"
	"github.com/pkg/errors"

	"github.com/buildpack/pack/internal/archive"
	"github.com/buildpack/pack/style"
)

const defaultAPI = "0.1"
const defaultLifecycleVersion = "0.3.0"

var DefaultLifecycleDescriptor = LifecycleDescriptor{
	Info: LifecycleInfo{
		Version: defaultLifecycleVersion,
	},
	API: LifecycleAPI{
		PlatformVersion:  defaultAPI,
		BuildpackVersion: defaultAPI,
	},
}

type Blob interface {
	Open() (io.ReadCloser, error)
}

type Lifecycle struct {
	descriptor LifecycleDescriptor
	Blob
}

type LifecycleDescriptor struct {
	Info LifecycleInfo `toml:"lifecycle"`
	API  LifecycleAPI  `toml:"api"`
}

type LifecycleInfo struct {
	Version string `toml:"version"`
}

type LifecycleAPI struct {
	PlatformVersion  string `toml:"platform"`
	BuildpackVersion string `toml:"buildpack"`
}

func (l *Lifecycle) Descriptor() LifecycleDescriptor {
	return l.descriptor
}

func NewLifecycle(blob Blob) (*Lifecycle, error) {
	br, err := blob.Open()
	if err != nil {
		return nil, errors.Wrap(err, "open lifecycle blob")
	}
	defer br.Close()

	var descriptor LifecycleDescriptor
	_, buf, err := archive.ReadTarEntry(br, "lifecycle.toml")

	//TODO: make lifecycle descriptor required after v0.4.0 release
	if err != nil && errors.Cause(err) == archive.ErrEntryNotExist {
		return &Lifecycle{
			Blob:       blob,
			descriptor: DefaultLifecycleDescriptor}, nil
	} else if err != nil {
		return nil, errors.Wrap(err, "decode lifecycle descriptor")
	}
	_, err = toml.Decode(string(buf), &descriptor)
	return &Lifecycle{Blob: blob, descriptor: descriptor}, nil
}

var lifecycleBinaries = []string{
	"detector",
	"restorer",
	"analyzer",
	"builder",
	"exporter",
	"cacher",
	"launcher",
}

func (l *Lifecycle) Validate(version *semver.Version) error {
	if err := l.validateVersion(version); err != nil {
		return err
	}
	return l.validateBinaries()
}

func (l *Lifecycle) validateBinaries() error {
	rc, err := l.Open()
	if err != nil {
		return errors.Wrap(err, "create lifecycle blob reader")
	}
	defer rc.Close()
	regex := regexp.MustCompile(`^[^/]+/([^/]+)$`)
	headers := map[string]bool{}
	tr := tar.NewReader(rc)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errors.Wrap(err, "failed to get next tar entry")
		}

		pathMatches := regex.FindStringSubmatch(path.Clean(header.Name))
		if pathMatches != nil {
			headers[pathMatches[1]] = true
		}
	}
	for _, p := range lifecycleBinaries {
		_, found := headers[p]
		if !found {
			return fmt.Errorf("did not find '%s' in tar", p)
		}
	}
	return nil
}

func (l *Lifecycle) validateVersion(version *semver.Version) error {
	actualVer, err := semver.NewVersion(l.descriptor.Info.Version)
	if err != nil {
		return errors.Wrapf(err, "lifecycle version %s is invalid semver", style.Symbol(l.descriptor.Info.Version))
	}
	if !actualVer.Equal(version) {
		return errors.Wrapf(err, "lifecycle has version %s which does not match provided version %s", style.Symbol(l.descriptor.Info.Version), style.Symbol(version.String()))
	}
	return nil
}
