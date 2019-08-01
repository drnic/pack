package blob

import (
	"fmt"
	"io/ioutil"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/Masterminds/semver"
	"github.com/pkg/errors"

	"github.com/buildpack/pack/internal/archive"
)

const (
	DefaultLifecycleVersion = "0.3.0"
)

//go:generate mockgen -package testmocks -destination testmocks/mock_downloader.go github.com/buildpack/pack/blob Downloader
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

func (f *Fetcher) FetchLifecycle(version *semver.Version, uri string) (Lifecycle, error) {
	if version == nil && uri == "" {
		version = semver.MustParse(DefaultLifecycleVersion)
	}

	if uri == "" {
		uri = fmt.Sprintf("https://github.com/buildpack/lifecycle/releases/download/v%s/lifecycle-v%s+linux.x86-64.tgz", version.String(), version.String())
	}

	path, err := f.downloader.Download(uri)
	if err != nil {
		return Lifecycle{}, errors.Wrapf(err, "retrieving lifecycle from %s", uri)
	}

	lifecycle := Lifecycle{Version: version, Blob: Blob{Path: path}}

	if err = lifecycle.validate(
		"detector",
		"restorer",
		"analyzer",
		"builder",
		"exporter",
		"cacher",
		"launcher",
	); err != nil {
		return Lifecycle{}, errors.Wrapf(err, "invalid lifecycle")
	}

	return lifecycle, nil
}
