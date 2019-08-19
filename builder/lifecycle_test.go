package builder_test

import (
	"archive/tar"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/Masterminds/semver"
	"github.com/fatih/color"
	"github.com/sclevine/spec"
	"github.com/sclevine/spec/report"

	"github.com/buildpack/pack/blob"
	"github.com/buildpack/pack/builder"
	h "github.com/buildpack/pack/testhelpers"
)

func TestLifecycle(t *testing.T) {
	color.NoColor = true
	spec.Run(t, "testLifecycle", testLifecycle, spec.Parallel(), spec.Report(report.Terminal{}))
}

func testLifecycle(t *testing.T, when spec.G, it spec.S) {
	when("#NewLifecycle", func() {
		it("makes a lifecycle from a blob", func() {
			lifecycle, err := builder.NewLifecycle(&blob.Blob{Path: filepath.Join("testdata", "lifecycle")})
			h.AssertNil(t, err)
			h.AssertEq(t, lifecycle.Descriptor().Info.Version, "1.2.3")
			h.AssertEq(t, lifecycle.Descriptor().API.PlatformVersion, "0.2")
			h.AssertEq(t, lifecycle.Descriptor().API.BuildpackVersion, "0.3")
		})

		when("there is no descriptor file", func() {
			it("assumes 0.1 API versions", func() {
				lifecycle, err := builder.NewLifecycle(&fakeEmptyBlob{})
				h.AssertNil(t, err)
				h.AssertEq(t, lifecycle.Descriptor().Info.Version, "0.3.0")
				h.AssertEq(t, lifecycle.Descriptor().API.PlatformVersion, "0.1")
				h.AssertEq(t, lifecycle.Descriptor().API.BuildpackVersion, "0.1")
			})
		})
	})

	when("#Validate", func() {
		when("lifecycle is valid", func() {
			it.Focus("succeeds", func() {
				lifecycle, err := builder.NewLifecycle(&blob.Blob{Path: filepath.Join("testdata", "lifecycle")})
				h.AssertNil(t, err)
				h.AssertNil(t, lifecycle.Validate(semver.MustParse("1.2.3")))
			})
		})

		when("the lifecycle has incomplete list of binaries", func() {
			var tmp string
			it.Before(func() {
				tmp, err := ioutil.TempDir("", "")
				h.AssertNil(t, err)

				h.AssertNil(t, ioutil.WriteFile(filepath.Join(tmp, "analyzer"), []byte("content"), os.ModePerm))
				h.AssertNil(t, ioutil.WriteFile(filepath.Join(tmp, "detector"), []byte("content"), os.ModePerm))
				h.AssertNil(t, ioutil.WriteFile(filepath.Join(tmp, "builder"), []byte("content"), os.ModePerm))
			})

			it.After(func() {
				h.AssertNil(t, os.RemoveAll(tmp))
			})

			it("returns an error", func() {
				lifecycle := &builder.Lifecycle{Blob: &blob.Blob{Path: tmp}}
				h.AssertError(t, lifecycle.Validate(semver.MustParse("1.2.3")), "invalid lifecycle")
			})
		})

		when("the versions don't match", func() {
			it("returns and error", func() {
				lifecycle := &builder.Lifecycle{Blob: &blob.Blob{Path: filepath.Join("testdata", "lifecycle")}}
				h.AssertError(t, lifecycle.Validate(semver.MustParse("4.5.6")), "lifecycle has version '1.2.3' which does not match provided version '4.5.6'")
			})
		})
	})
}

type fakeEmptyBlob struct {
}

func (f *fakeEmptyBlob) Open() (io.ReadCloser, error) {
	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		tw := tar.NewWriter(pw)
		defer tw.Close()
	}()
	return pr, nil
}
