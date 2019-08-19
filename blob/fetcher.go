package blob

import (
	"github.com/Masterminds/semver"
	"github.com/pkg/errors"
)

const (
	DefaultLifecycleVersion = "0.3.0"
)

//go:generate mockgen -package testmocks -destination testmocks/mock_downloader.go github.com/buildpack/pack/blob Downloader
type Downloader interface {
	Download(uri string) (*Blob, error)
}

type Fetcher struct {
	downloader Downloader
}

func NewFetcher(downloader Downloader) *Fetcher {
	return &Fetcher{downloader: downloader}
}

func (f *Fetcher) FetchBuildpack(uri string) (Buildpack, error) {
	blob, err := f.downloader.Download(uri)
	if err != nil {
		return Buildpack{}, errors.Wrap(err, "fetching buildpack")
	}

	bp, err := NewBuildpack(blob)
	if err != nil {
		return Buildpack{}, err
	}
	return bp, nil
}

func (f *Fetcher) FetchLifecycle(version *semver.Version, uri string) (Lifecycle, error) {
	if uri == "" && version == nil {
		version = semver.MustParse(DefaultLifecycleVersion)
	}
	if uri == "" {
		uri = uriFromLifecycleVersion(version)
	}

	blob, err := f.downloader.Download(uri)
	if err != nil {
		return Lifecycle{}, errors.Wrapf(err, "retrieving lifecycle from %s", uri)
	}

	lifecycle := Lifecycle{Version: version, Blob: blob}

	if err = lifecycle.validate(); err != nil {
		return Lifecycle{}, errors.Wrapf(err, "invalid lifecycle")
	}

	return lifecycle, nil
}
