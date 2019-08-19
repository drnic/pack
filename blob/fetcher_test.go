package blob_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/Masterminds/semver"
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
		var buildpackTgz string

		it.Before(func() {
			buildpackTgz = h.CreateTGZ(t, filepath.Join("testdata", "buildpack"), "./", 0644)
		})

		it.After(func() {
			h.AssertNil(t, os.Remove(buildpackTgz))
		})

		it("fetches a buildpack", func() {
			downloadPath := filepath.Join("testdata", "buildpack")
			mockDownloader.EXPECT().
				Download(downloadPath).
				Return(&blob.Blob{Path: downloadPath}, nil)

			out, err := subject.FetchBuildpack(downloadPath)
			h.AssertNil(t, err)
			h.AssertEq(t, out.Info.ID, "bp.one")
			h.AssertEq(t, out.Info.Version, "bp.one.version")
			h.AssertEq(t, out.Order[0].Group[0].ID, "bp.nested")
			h.AssertEq(t, out.Order[0].Group[0].Version, "bp.nested.version")
			h.AssertEq(t, out.Stacks[0].ID, "some.stack.id")
			h.AssertEq(t, out.Stacks[1].ID, "other.stack.id")
			h.AssertEq(t, out.Blob.Path, downloadPath)
		})
	})

	when("#FetchLifecycle", func() {
		var lifecycleBlob *blob.Blob

		it.Before(func() {
			lifecycleBlob = &blob.Blob{
				Path: h.CreateTGZ(t, filepath.Join("testdata", "lifecycle"), "./lifecycle", 0755),
			}
		})

		it.After(func() {
			h.AssertNil(t, os.Remove(lifecycleBlob.Path))
		})

		when("#FetchLifecycle", func() {
			when("only a version is provided", func() {
				it("returns a release from github", func() {
					mockDownloader.EXPECT().
						Download("https://github.com/buildpack/lifecycle/releases/download/v1.2.3/lifecycle-v1.2.3+linux.x86-64.tgz").
						Return(lifecycleBlob, nil)

					md, err := subject.FetchLifecycle(semver.MustParse("1.2.3"), "")
					h.AssertNil(t, err)
					h.AssertEq(t, md.Version.String(), "1.2.3")
					h.AssertEq(t, md.Blob, lifecycleBlob)
				})
			})

			when("only a uri is provided", func() {
				it("returns the lifecycle from the uri", func() {
					mockDownloader.EXPECT().
						Download("https://lifecycle.example.com").
						Return(lifecycleBlob, nil)

					md, err := subject.FetchLifecycle(nil, "https://lifecycle.example.com")
					h.AssertNil(t, err)
					h.AssertNil(t, md.Version)
					h.AssertEq(t, md.Blob, lifecycleBlob)
				})
			})

			when("a uri and version are provided", func() {
				it("returns the lifecycle from the uri", func() {
					mockDownloader.EXPECT().
						Download("https://lifecycle.example.com").
						Return(lifecycleBlob, nil)

					md, err := subject.FetchLifecycle(semver.MustParse("1.2.3"), "https://lifecycle.example.com")
					h.AssertNil(t, err)
					h.AssertEq(t, md.Version.String(), "1.2.3")
					h.AssertEq(t, md.Blob, lifecycleBlob)
				})
			})

			when("neither is uri nor version is provided", func() {
				it("returns the default lifecycle", func() {
					mockDownloader.EXPECT().
						Download(fmt.Sprintf(
							"https://github.com/buildpack/lifecycle/releases/download/v%s/lifecycle-v%s+linux.x86-64.tgz",
							blob.DefaultLifecycleVersion,
							blob.DefaultLifecycleVersion,
						)).
						Return(lifecycleBlob, nil)

					md, err := subject.FetchLifecycle(nil, "")
					h.AssertNil(t, err)
					h.AssertEq(t, md.Version.String(), blob.DefaultLifecycleVersion)
					h.AssertEq(t, md.Blob, lifecycleBlob)
				})
			})

			when("the lifecycle is missing binaries", func() {
				it("returns an error", func() {
					tmp, err := ioutil.TempDir("", "")
					h.AssertNil(t, err)
					defer os.RemoveAll(tmp)

					mockDownloader.EXPECT().
						Download(fmt.Sprintf(
							"https://github.com/buildpack/lifecycle/releases/download/v%s/lifecycle-v%s+linux.x86-64.tgz",
							blob.DefaultLifecycleVersion,
							blob.DefaultLifecycleVersion,
						)).
						Return(&blob.Blob{Path: tmp}, nil)

					_, err = subject.FetchLifecycle(nil, "")
					h.AssertError(t, err, "invalid lifecycle")
				})
			})

			when("the lifecycle has incomplete list of binaries", func() {
				it("returns an error", func() {
					tmp, err := ioutil.TempDir("", "")
					h.AssertNil(t, err)
					defer os.RemoveAll(tmp)

					h.AssertNil(t, ioutil.WriteFile(filepath.Join(tmp, "analyzer"), []byte("content"), os.ModePerm))
					h.AssertNil(t, ioutil.WriteFile(filepath.Join(tmp, "detector"), []byte("content"), os.ModePerm))
					h.AssertNil(t, ioutil.WriteFile(filepath.Join(tmp, "builder"), []byte("content"), os.ModePerm))

					mockDownloader.EXPECT().
						Download(fmt.Sprintf(
							"https://github.com/buildpack/lifecycle/releases/download/v%s/lifecycle-v%s+linux.x86-64.tgz",
							blob.DefaultLifecycleVersion,
							blob.DefaultLifecycleVersion,
						)).
						Return(&blob.Blob{Path: tmp}, nil)

					_, err = subject.FetchLifecycle(nil, "")
					h.AssertError(t, err, "invalid lifecycle")
				})
			})
		})
	})
}
