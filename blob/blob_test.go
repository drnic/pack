package blob_test

import (
	"archive/tar"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/sclevine/spec"
	"github.com/sclevine/spec/report"

	"github.com/buildpack/pack/blob"
	"github.com/buildpack/pack/builder"
	h "github.com/buildpack/pack/testhelpers"
)

func TestBlob(t *testing.T) {
	spec.Run(t, "Buildpack", testBlob, spec.Parallel(), spec.Report(report.Terminal{}))
}

func testBlob(t *testing.T, when spec.G, it spec.S) {
	when("#Blob", func() {
		when("#Open", func() {
			var (
				blobDir  = filepath.Join("testdata", "blob")
				blobPath string
			)

			when("dir", func() {
				it.Before(func() {
					blobPath = blobDir
				})
				it("returns a tar reader", func() {
					testBlobOpen(t, blob.NewBlob(blobPath))
				})
			})

			when("tgz", func() {
				it.Before(func() {
					blobPath = h.CreateTGZ(t, blobDir, ".", -1)
				})

				it.After(func() {
					h.AssertNil(t, os.Remove(blobPath))
				})
				it("returns a tar reader", func() {
					testBlobOpen(t, blob.NewBlob(blobPath))
				})
			})

			when("tar", func() {
				it.Before(func() {
					blobPath = h.CreateTAR(t, blobDir, ".", -1)
				})

				it.After(func() {
					h.AssertNil(t, os.Remove(blobPath))
				})
				it("returns a tar reader", func() {
					testBlobOpen(t, blob.NewBlob(blobPath))
				})
			})
		})
	})
}

func testBlobOpen(t *testing.T, blob builder.Blob) {
	rc, err := blob.Open()
	h.AssertNil(t, err)
	defer rc.Close()
	tr := tar.NewReader(rc)
	header, err := tr.Next()
	h.AssertNil(t, err)
	h.AssertEq(t, header.Name, "file.txt")
	contents := make([]byte, header.FileInfo().Size(), header.FileInfo().Size())
	_, err = tr.Read(contents)
	h.AssertSameInstance(t, err, io.EOF)
	h.AssertEq(t, string(contents), "contents")
}
