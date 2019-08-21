package blob

import (
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

func (f *Fetcher) FetchBuildpack(uri string) (builder.Buildpack, error) {
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
