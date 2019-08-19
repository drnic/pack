package blob

import (
	"fmt"

	"github.com/Masterminds/semver"
	"github.com/pkg/errors"

	"github.com/buildpack/pack/builder"
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

func (f *Fetcher) FetchBuildpack(uri string) (*builder.Buildpack, error) {
	blob, err := f.downloader.Download(uri)
	if err != nil {
		return nil, errors.Wrap(err, "fetching buildpack")
	}

	bp, err := builder.NewBuildpack(blob)
	if err != nil {
		return nil, err
	}
	return bp, nil
}

func uriFromLifecycleVersion(version *semver.Version) string {
	if version == nil {
		version = semver.MustParse(DefaultLifecycleVersion)
	}

	return fmt.Sprintf("https://github.com/buildpack/lifecycle/releases/download/v%s/lifecycle-v%s+linux.x86-64.tgz", version.String(), version.String())
}

func (f *Fetcher) FetchLifecycle(version *semver.Version, uri string) (*builder.Lifecycle, error) {
	if uri == "" && version == nil {
		version = semver.MustParse(DefaultLifecycleVersion)
	}
	if uri == "" {
		uri = uriFromLifecycleVersion(version)
	}

	blob, err := f.downloader.Download(uri)
	if err != nil {
		return nil, errors.Wrapf(err, "retrieving lifecycle from %s", uri)
	}

	lifecycle, err := builder.NewLifecycle(blob)
	if err != nil {
		return nil, err
	}

	if err = lifecycle.Validate(version); err != nil {
		return nil, errors.Wrapf(err, "invalid lifecycle")
	}

	return lifecycle, nil
}
