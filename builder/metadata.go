package builder

import (
	"github.com/buildpack/pack/blob"
)

const MetadataLabel = "io.buildpacks.builder.metadata"

type Metadata struct {
	Description string              `json:"description"`
	Buildpacks  []BuildpackMetadata `json:"buildpacks"`
	Groups      V1Order             `json:"groups"` // deprecated
	Stack       StackMetadata       `json:"stack"`
	Lifecycle   blob.Lifecycle      `json:"lifecycle"`
}

type BuildpackMetadata struct {
	blob.BuildpackInfo
	Latest bool `json:"latest"` // deprecated
}

type StackMetadata struct {
	RunImage RunImageMetadata `json:"runImage" toml:"run-image"`
}

type RunImageMetadata struct {
	Image   string   `json:"image" toml:"image"`
	Mirrors []string `json:"mirrors" toml:"mirrors"`
}

func processMetadata(md *Metadata) error {
	for i, bp := range md.Buildpacks {
		var matchingBps []blob.BuildpackInfo
		for _, bp2 := range md.Buildpacks {
			if bp.ID == bp2.ID {
				matchingBps = append(matchingBps, bp.BuildpackInfo)
			}
		}

		if len(matchingBps) == 1 {
			md.Buildpacks[i].Latest = true
		}
	}

	return nil
}
