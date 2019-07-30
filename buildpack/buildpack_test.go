package buildpack

import (
	"archive/tar"
	"os"
	"path/filepath"
	"testing"

	"github.com/sclevine/spec"
	"github.com/sclevine/spec/report"

	h "github.com/buildpack/pack/testhelpers"
)

func TestBuildpack(t *testing.T) {
	spec.Run(t, "Buildpack", testBuildpack, spec.Parallel(), spec.Report(report.Terminal{}))
}

func testBuildpack(t *testing.T, when spec.G, it spec.S) {
	when("#Buildpack", func() {
		when("#Read", func() {
			var bpDir = filepath.Join("testdata", "buildpack")

			when("bp path is a dir", func() {
				it("returns a tar reader", func() {
					bp := &Buildpack{
						Blob: Blob{Path: bpDir},
					}
					r, err := bp.Read()
					h.AssertNil(t, err)
					tr := tar.NewReader(r)
					header, err := tr.Next()
					h.AssertNil(t, err)
					h.AssertEq(t, header.Name, "bin")
					header, err = tr.Next()
					h.AssertNil(t, err)
					h.AssertEq(t, header.Name, "bin/build")
					header, err = tr.Next()
					h.AssertNil(t, err)
					h.AssertEq(t, header.Name, "bin/detect")
					header, err = tr.Next()
					h.AssertNil(t, err)
					h.AssertEq(t, header.Name, "buildpack.toml")
				})
			})

			when("bp path is a tgz", func() {
				var (
					bpTGZ string
				)

				it.Before(func() {
					bpTGZ = h.CreateTGZ(t, bpDir, ".", -1)
				})

				it.After(func() {
					h.AssertNil(t, os.Remove(bpTGZ))
				})

				it("returns a tar reader", func() {
					bp := &Buildpack{
						Blob: Blob{Path: bpTGZ},
					}
					r, err := bp.Read()
					h.AssertNil(t, err)
					tr := tar.NewReader(r)
					header, err := tr.Next()
					h.AssertNil(t, err)
					h.AssertEq(t, header.Name, "bin")
					header, err = tr.Next()
					h.AssertNil(t, err)
					h.AssertEq(t, header.Name, "bin/build")
					header, err = tr.Next()
					h.AssertNil(t, err)
					h.AssertEq(t, header.Name, "bin/detect")
					header, err = tr.Next()
					h.AssertNil(t, err)
					h.AssertEq(t, header.Name, "buildpack.toml")
				})
			})

			when("bp path is a tar", func() {
				var (
					bpTAR string
				)

				it.Before(func() {
					bpTAR = h.CreateTAR(t, bpDir, ".", -1)
				})

				it.After(func() {
					h.AssertNil(t, os.Remove(bpTAR))
				})

				it("returns a tar reader", func() {
					bp := &Buildpack{
						Blob: Blob{Path: bpTAR},
					}
					r, err := bp.Read()
					h.AssertNil(t, err)
					tr := tar.NewReader(r)
					header, err := tr.Next()
					h.AssertNil(t, err)
					h.AssertEq(t, header.Name, "bin")
					header, err = tr.Next()
					h.AssertNil(t, err)
					h.AssertEq(t, header.Name, "bin/build")
					header, err = tr.Next()
					h.AssertNil(t, err)
					h.AssertEq(t, header.Name, "bin/detect")
					header, err = tr.Next()
					h.AssertNil(t, err)
					h.AssertEq(t, header.Name, "buildpack.toml")
				})
			})
		})
	})
}
