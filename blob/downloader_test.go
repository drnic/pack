package blob_test

import (
	"testing"

	"github.com/sclevine/spec"
	"github.com/sclevine/spec/report"
)

func TestDownloader(t *testing.T) {
	spec.Run(t, "Downloader", testDownloader, spec.Sequential(), spec.Report(report.Terminal{}))
}

// FIXME: how do we determine the path from an abstract class?
func testDownloader(t *testing.T, when spec.G, it spec.S) {
	when.Pend("#Download", func() {
		//	var (
		//		cacheDir string
		//		err      error
		//		subject  blob.Downloader
		//	)
		//
		//	it.Before(func() {
		//		cacheDir, err = ioutil.TempDir("", "cache")
		//		h.AssertNil(t, err)
		//		subject = blob.NewDownloader(logging.New(ioutil.Discard), cacheDir)
		//	})
		//
		//	it.After(func() {
		//		h.AssertNil(t, os.RemoveAll(cacheDir))
		//	})
		//
		//	when("is path", func() {
		//		var (
		//			//relPath string
		//			//absPath string
		//		)
		//		it.Before(func() {
		//			//relPath = filepath.Join("testdata", "blob")
		//			//absPath, err = filepath.Abs(relPath)
		//			//h.AssertNil(t, err)
		//		})
		//
		//		when("is absolute", func() {
		//			it("return the absolute path", func() {
		//				//blob, err := subject.Download(absPath)
		//				//h.AssertNil(t, err)
		//				//h.AssertEq(t, blob.Path, absPath)
		//			})
		//		})
		//
		//		when("is relative", func() {
		//			it("resolves the absolute path", func() {
		//				//blob, err := subject.Download(relPath)
		//				//h.AssertNil(t, err)
		//				//h.AssertEq(t, blob.Path, absPath)
		//			})
		//		})
		//
		//		when("path is a file:// uri", func() {
		//			it("resolves the absolute path", func() {
		//				//uri, err := paths.FilePathToUri(absPath)
		//				//h.AssertNil(t, err)
		//
		//				//blob, err := subject.Download(uri)
		//				//h.AssertNil(t, err)
		//				//h.AssertEq(t, blob.Path, absPath)
		//			})
		//		})
		//	})
		//
		//	when("is uri", func() {
		//		var (
		//			server            *ghttp.Server
		//			uri               string
		//			expectedCachePath string
		//			tgz               string
		//		)
		//
		//		it.Before(func() {
		//			server = ghttp.NewServer()
		//			uri = server.URL() + "/downloader/somefile.tgz"
		//
		//			tgz = h.CreateTGZ(t, filepath.Join("testdata", "blob"), "./", 0777)
		//			server.AppendHandlers(func(w http.ResponseWriter, r *http.Request) {
		//				w.Header().Add("ETag", "A")
		//				http.ServeFile(w, r, tgz)
		//			})
		//
		//			//Second call errors to test cache use
		//			server.AppendHandlers(func(w http.ResponseWriter, r *http.Request) {
		//				w.WriteHeader(304)
		//			})
		//
		//			expectedCachePath = filepath.Join(cacheDir,
		//				"c2",
		//				fmt.Sprintf("%x", sha256.Sum256([]byte(uri))),
		//			)
		//		})
		//
		//		it.After(func() {
		//			os.Remove(tgz)
		//			server.Close()
		//		})
		//
		//		it("downloads from a 'http(s)://' URI", func() {
		//			blob, err := subject.Download(uri)
		//			h.AssertNil(t, err)
		//			//h.AssertEq(t, blob.Path, expectedCachePath)
		//			testBlobOpen(t, blob)
		//		})
		//
		//		it("uses cache from a 'http(s)://' URI tgz", func() {
		//			blob, err := subject.Download(uri)
		//			h.AssertNil(t, err)
		//			//h.AssertEq(t, blob.Path, expectedCachePath)
		//			testBlobOpen(t, blob)
		//			blob, err = subject.Download(uri)
		//			//h.AssertEq(t, blob.Path, expectedCachePath)
		//			testBlobOpen(t, blob)
		//		})
		//	})
	})
}
