package blob_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/sclevine/spec"
	"github.com/sclevine/spec/report"

	"github.com/buildpack/pack/blob"
	"github.com/buildpack/pack/blob/testmocks"
	h "github.com/buildpack/pack/testhelpers"
)

func TestFetcher(t *testing.T) {
	spec.Run(t, "Fetcher", fetcher, spec.Parallel(), spec.Report(report.Terminal{}))
}

func fetcher(t *testing.T, when spec.G, it spec.S) {
	var (
		mockController *gomock.Controller
		mockDownloader *testmocks.MockDownloader
		subject        *blob.Fetcher
	)

	it.Before(func() {
		mockController = gomock.NewController(t)
		mockDownloader = testmocks.NewMockDownloader(mockController)

		subject = blob.NewFetcher(mockDownloader)
	})

	it.After(func() {
		mockController.Finish()
	})

	when("#FetchBuildpack", func() {
		var buildpackBlob *blob.Blob

		it.Before(func() {
			buildpackBlob = &blob.Blob{
				Path: h.CreateTGZ(t, filepath.Join("testdata", "buildpack"), "./", 0755),
			}
		})

		it.After(func() {
			h.AssertNil(t, os.Remove(buildpackBlob.Path))
		})

		it("fetches a buildpack", func() {
			mockDownloader.EXPECT().
				Download(buildpackBlob.Path).
				Return(buildpackBlob, nil)

			bp, err := subject.FetchBuildpack(buildpackBlob.Path)
			h.AssertNil(t, err)
			descriptor := bp.Descriptor()
			h.AssertEq(t, descriptor.Info.ID, "bp.one")
			h.AssertEq(t, descriptor.Info.Version, "bp.one.version")
			h.AssertEq(t, descriptor.Order[0].Group[0].ID, "bp.nested")
			h.AssertEq(t, descriptor.Order[0].Group[0].Version, "bp.nested.version")
			h.AssertEq(t, descriptor.Stacks[0].ID, "some.stack.id")
			h.AssertEq(t, descriptor.Stacks[1].ID, "other.stack.id")
		})
	})
}
