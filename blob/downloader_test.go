package blob_test

import (
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/onsi/gomega/ghttp"

	"github.com/buildpack/pack"
	"github.com/buildpack/pack/blob"
	"github.com/buildpack/pack/internal/archive"
	"github.com/buildpack/pack/internal/paths"
	"github.com/buildpack/pack/logging"

	"github.com/sclevine/spec"
	"github.com/sclevine/spec/report"

	h "github.com/buildpack/pack/testhelpers"
)

func TestDownloader(t *testing.T) {
	spec.Run(t, "Downloader", testDownloader, spec.Sequential(), spec.Report(report.Terminal{}))
}

func testDownloader(t *testing.T, when spec.G, it spec.S) {
	when("#Download", func() {
		var (
			cacheDir string
			err      error
			subject  pack.Downloader
		)

		it.Before(func() {
			cacheDir, err = ioutil.TempDir("", "cache")
			h.AssertNil(t, err)
			subject = blob.NewDownloader(logging.New(ioutil.Discard), cacheDir)
		})

		it.After(func() {
			h.AssertNil(t, os.RemoveAll(cacheDir))
		})

		when("is path", func() {
			var (
				relPath string
			)

			it.Before(func() {
				relPath = filepath.Join("testdata", "blob")
			})

			when("is absolute", func() {
				it("return the absolute path", func() {
					absPath, err := filepath.Abs(relPath)
					h.AssertNil(t, err)

					b, err := subject.Download(absPath)
					h.AssertNil(t, err)
					assertBlob(t, b)
				})
			})

			when("is relative", func() {
				it("resolves the absolute path", func() {
					b, err := subject.Download(relPath)
					h.AssertNil(t, err)
					assertBlob(t, b)
				})
			})

			when("path is a file:// uri", func() {
				it("resolves the absolute path", func() {
					absPath, err := filepath.Abs(relPath)
					h.AssertNil(t, err)

					uri, err := paths.FilePathToUri(absPath)
					h.AssertNil(t, err)

					b, err := subject.Download(uri)
					h.AssertNil(t, err)
					assertBlob(t, b)
				})
			})
		})

		when("is uri", func() {
			var (
				server *ghttp.Server
				uri    string
				tgz    string
			)

			it.Before(func() {
				server = ghttp.NewServer()
				uri = server.URL() + "/downloader/somefile.tgz"

				tgz = h.CreateTGZ(t, filepath.Join("testdata", "blob"), "./", 0777)
			})

			it.After(func() {
				os.Remove(tgz)
				server.Close()
			})

			when("uri is valid", func() {
				it.Before(func() {
					server.AppendHandlers(func(w http.ResponseWriter, r *http.Request) {
						w.Header().Add("ETag", "A")
						http.ServeFile(w, r, tgz)
					})

					server.AppendHandlers(func(w http.ResponseWriter, r *http.Request) {
						w.WriteHeader(304)
					})
				})

				it("downloads from a 'http(s)://' URI", func() {
					b, err := subject.Download(uri)
					h.AssertNil(t, err)
					testBlobOpen(t, b)
				})

				it("uses cache from a 'http(s)://' URI tgz", func() {
					b, err := subject.Download(uri)
					h.AssertNil(t, err)
					assertBlob(t, b)

					b, err = subject.Download(uri)
					h.AssertNil(t, err)
					assertBlob(t, b)
				})
			})

			when("uri is invalid", func() {
				when("uri file is not found", func() {
					it.Before(func() {
						server.AppendHandlers(func(w http.ResponseWriter, r *http.Request) {
							w.WriteHeader(404)
						})
					})

					it("should return error", func() {
						_, err := subject.Download(uri)
						h.AssertError(t, err, "could not download")
						h.AssertError(t, err, "http status '404'")
					})
				})

				when("uri is unsupported", func() {
					it("should return error", func() {
						_, err := subject.Download("not-supported://file.tgz")
						h.AssertError(t, err, "unsupported protocol 'not-supported'")
					})
				})
			})
		})
	})
}

func assertBlob(t *testing.T, b blob.Blob) {
	t.Helper()
	r, err := b.Open()
	h.AssertNil(t, err)

	_, bytes, err := archive.ReadTarEntry(r, "file.txt")
	h.AssertNil(t, err)

	h.AssertEq(t, string(bytes), "contents")
}
