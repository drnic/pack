package blob

import (
	"archive/tar"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/sclevine/spec"
	"github.com/sclevine/spec/report"

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

			testBlob := func() {
				blob := &Blob{Path: blobPath}
				r, err := blob.Open()
				h.AssertNil(t, err)
				defer r.Close()
				tr := tar.NewReader(r)
				header, err := tr.Next()
				h.AssertNil(t, err)
				h.AssertEq(t, header.Name, "file.txt")
				contents := make([]byte, header.FileInfo().Size(), header.FileInfo().Size())
				_, err = tr.Read(contents)
				h.AssertSameInstance(t, err, io.EOF)
				h.AssertEq(t, string(contents), "contents")
			}

			when("dir", func() {
				it.Before(func() {
					blobPath = blobDir
				})
				it("returns a tar reader", testBlob)
			})

			when("tgz", func() {
				it.Before(func() {
					blobPath = h.CreateTGZ(t, blobDir, ".", -1)
				})

				it.After(func() {
					h.AssertNil(t, os.Remove(blobPath))
				})
				it("returns a tar reader", testBlob)
			})

			when("tar", func() {
				it.Before(func() {
					blobPath = h.CreateTAR(t, blobDir, ".", -1)
				})

				it.After(func() {
					h.AssertNil(t, os.Remove(blobPath))
				})
				it("returns a tar reader", testBlob)
			})
		})
	})
}
