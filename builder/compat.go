package builder

import (
	"archive/tar"
	"fmt"

	"github.com/pkg/errors"
)

type V1Order []V1Group

type V1Group struct {
	Buildpacks []BuildpackRef `toml:"buildpacks" json:"buildpacks"`
}

type v1OrderTOML struct {
	Groups []V1Group `toml:"groups" json:"groups"`
}

func (o V1Order) ToOrder() Order {
	var order Order
	for _, gp := range o {
		var buildpacks []BuildpackRef
		for _, bp := range gp.Buildpacks {
			buildpacks = append(buildpacks, bp)
		}

		order = append(order, OrderEntry{
			Group: buildpacks,
		})
	}

	return order
}

func (o Order) ToV1Order() V1Order {
	var order V1Order
	for _, gp := range o {
		var buildpacks []BuildpackRef
		for _, bp := range gp.Group {
			buildpacks = append(buildpacks, bp)
		}

		order = append(order, V1Group{
			Buildpacks: buildpacks,
		})
	}

	return order
}

// Deprecated: The 'latest' symlink is in place for backwards compatibility only. This should be removed as soon
// as we no longer support older releases that rely on it.
func symlinkLatest(tw *tar.Writer, baseTarDir string, bp Buildpack, metadata Metadata) error {
	for _, b := range metadata.Buildpacks {
		if b.ID == bp.Info.ID && b.Version == bp.Info.Version && b.Latest {
			err := tw.WriteHeader(&tar.Header{
				Name:     fmt.Sprintf("%s/%s/%s", buildpacksDir, bp.EscapedID(), "latest"),
				Linkname: baseTarDir,
				Typeflag: tar.TypeSymlink,
				Mode:     0644,
			})
			if err != nil {
				return errors.Wrapf(err, "creating latest symlink for buildpack '%s:%s'", bp.Info.ID, bp.Info.Version)
			}
			break
		}
	}
	return nil
}
