package blob

import (
	"archive/tar"
	"fmt"
	"io"
	"path"
	"regexp"

	"github.com/Masterminds/semver"
	"github.com/pkg/errors"
)

type Lifecycle struct {
	Version *semver.Version
	*Blob
}

var lifeccycleBinaries = []string{
	"detector",
	"restorer",
	"analyzer",
	"builder",
	"exporter",
	"cacher",
	"launcher",
}

func uriFromLifecycleVersion(version *semver.Version) string {
	if version == nil {
		version = semver.MustParse(DefaultLifecycleVersion)
	}

	return fmt.Sprintf("https://github.com/buildpack/lifecycle/releases/download/v%s/lifecycle-v%s+linux.x86-64.tgz", version.String(), version.String())
}

func (l *Lifecycle) validate() error {
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

	for _, p := range lifeccycleBinaries {
		_, found := headers[p]
		if !found {
			return fmt.Errorf("did not find '%s' in tar", p)
		}
	}
	return nil
}
