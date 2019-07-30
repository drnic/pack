package blob

import (
	"io/ioutil"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/pkg/errors"

	"github.com/buildpack/pack/internal/archive"
)

//go:generate mockgen -package mocks -destination mocks/downloader.go github.com/buildpack/pack/lifecycle Downloader

type Downloader interface {
	Download(uri string) (string, error)
}

type Fetcher struct {
	downloader Downloader
}

func NewFetcher(downloader Downloader) *Fetcher {
	return &Fetcher{downloader: downloader}
}

func (f *Fetcher) FetchBuildpack(uri string) (Buildpack, error) {
	downloadedPath, err := f.downloader.Download(uri)
	if err != nil {
		return Buildpack{}, errors.Wrap(err, "fetching buildpack")
	}

	bp, err := readBuildpackTOML(downloadedPath)
	if err != nil {
		return Buildpack{}, err
	}
	bp.Blob = Blob{Path: downloadedPath}
	return bp, nil
}

func readBuildpackTOML(path string) (Buildpack, error) {
	var (
		buf []byte
		err error
	)
	if filepath.Ext(path) == ".tgz" {
		_, buf, err = archive.ReadTarEntry(path, "./buildpack.toml", "buildpack.toml", "/buildpack.toml")
	} else {
		buf, err = ioutil.ReadFile(filepath.Join(path, "buildpack.toml"))
	}

	if err != nil {
		return Buildpack{}, err
	}

	bp := Buildpack{}
	_, err = toml.Decode(string(buf), &bp)
	if err != nil {
		return Buildpack{}, errors.Wrapf(err, "reading buildpack.toml from path %s", path)
	}
	return bp, nil
}
