package blob

import (
	"crypto/sha256"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"github.com/pkg/errors"

	"github.com/buildpack/pack/internal/paths"
	"github.com/buildpack/pack/logging"
)

const (
	cacheDirPrefix = "c"
	cacheVersion   = "2"
)

type downloader struct {
	logger       logging.Logger
	baseCacheDir string
}

func NewDownloader(logger logging.Logger, baseCacheDir string) *downloader {
	return &downloader{
		logger:       logger,
		baseCacheDir: baseCacheDir,
	}
}

func (d *downloader) Download(pathOrUri string) (*Blob, error) {
	var (
		path string
		err  error
	)
	if paths.IsURI(pathOrUri) {
		parsedUrl, err := url.Parse(pathOrUri)
		if err != nil {
			return nil, err
		}

		switch parsedUrl.Scheme {
		case "file":
			path, err = paths.UriToFilePath(pathOrUri)
		case "http", "https":
			path, err = d.handleHTTP(pathOrUri)
		default:
			return nil, fmt.Errorf("unsupported protocol '%s' in URI %q", parsedUrl.Scheme, pathOrUri)
		}
	} else {
		path, err = d.handleFile(pathOrUri)
	}
	if err != nil {
		return nil, err
	}
	return &Blob{Path: path}, nil
}

func (d *downloader) handleFile(path string) (string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", nil
	}

	return path, nil
}

func (d *downloader) handleHTTP(uri string) (string, error) {
	cacheDir := d.versionedCacheDir()

	if err := os.MkdirAll(cacheDir, 0744); err != nil {
		return "", err
	}

	cachePath := filepath.Join(cacheDir, fmt.Sprintf("%x", sha256.Sum256([]byte(uri))))

	etagFile := cachePath + ".etag"
	etagExists, err := fileExists(etagFile)
	if err != nil {
		return "", err
	}

	etag := ""
	if etagExists {
		bytes, err := ioutil.ReadFile(etagFile)
		if err != nil {
			return "", err
		}
		etag = string(bytes)
	}

	reader, etag, err := d.downloadAsStream(uri, etag)
	if err != nil {
		return "", errors.Wrapf(err, "failed to download from %q", uri)
	} else if reader == nil {
		return cachePath, nil
	}
	defer reader.Close()

	fh, err := os.Create(cachePath)
	if err != nil {
		return "", err
	}
	defer fh.Close()

	_, err = io.Copy(fh, reader)
	if err != nil {
		return "", err
	}

	if err = ioutil.WriteFile(etagFile, []byte(etag), 0744); err != nil {
		return "", err
	}

	return cachePath, nil
}

func (d *downloader) downloadAsStream(uri string, etag string) (io.ReadCloser, string, error) {
	req, err := http.NewRequest("GET", uri, nil)
	if err != nil {
		return nil, "", err
	}

	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return nil, "", err
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		d.logger.Debugf("Downloading from %q", uri)
		return resp.Body, resp.Header.Get("Etag"), nil
	}

	if resp.StatusCode == 304 {
		d.logger.Debugf("Using cached version of %q", uri)
		return nil, etag, nil
	}

	return nil, "", fmt.Errorf("could not download from %q, code http status %d", uri, resp.StatusCode)
}

func (d *downloader) versionedCacheDir() string {
	return filepath.Join(d.baseCacheDir, cacheDirPrefix+cacheVersion)
}

func fileExists(file string) (bool, error) {
	_, err := os.Stat(file)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
