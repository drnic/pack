package builder_test

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Masterminds/semver"
	"github.com/buildpack/imgutil/fakes"
	"github.com/fatih/color"
	"github.com/golang/mock/gomock"
	"github.com/sclevine/spec"
	"github.com/sclevine/spec/report"

	"github.com/buildpack/pack/builder"
	"github.com/buildpack/pack/builder/testmocks"
	"github.com/buildpack/pack/internal/archive"
	h "github.com/buildpack/pack/testhelpers"
)

func TestBuilder(t *testing.T) {
	color.NoColor = true
	spec.Run(t, "Builder", testBuilder, spec.Parallel(), spec.Report(report.Terminal{}))
}

func testBuilder(t *testing.T, when spec.G, it spec.S) {
	var (
		baseImage      *fakes.Image
		subject        *builder.Builder
		mockController *gomock.Controller
		mockLifecycle  *testmocks.MockLifecycle
		bp1v1          builder.Buildpack
		bp1v2          builder.Buildpack
		bp2v1          builder.Buildpack
		bpOrder        builder.Buildpack
	)

	it.Before(func() {
		baseImage = fakes.NewImage("base/image", "", "")
		mockController = gomock.NewController(t)
		mockLifecycle = testmocks.NewMockLifecycle(mockController)
		mockLifecycle.EXPECT().Open().Return(archive.ReadDirAsTar(
			filepath.Join("testdata", "lifecycle"), ".", 0, 0, -1), nil).AnyTimes()
		bp1v1 = &fakeBuildpack{descriptor: builder.BuildpackDescriptor{
			Info: builder.BuildpackInfo{
				ID:      "buildpack-1-id",
				Version: "buildpack-1-version-1",
			},
			Stacks: []builder.Stack{{ID: "some.stack.id"}},
		}}
		bp1v2 = &fakeBuildpack{descriptor: builder.BuildpackDescriptor{
			Info: builder.BuildpackInfo{
				ID:      "buildpack-1-id",
				Version: "buildpack-1-version-2",
			},
			Stacks: []builder.Stack{{ID: "some.stack.id"}},
		}}
		bp2v1 = &fakeBuildpack{descriptor: builder.BuildpackDescriptor{
			Info: builder.BuildpackInfo{
				ID:      "buildpack-2-id",
				Version: "buildpack-2-version-1",
			},
			Stacks: []builder.Stack{{ID: "some.stack.id"}},
		}}
		bpOrder = &fakeBuildpack{descriptor: builder.BuildpackDescriptor{
			Info: builder.BuildpackInfo{
				ID:      "order-buildpack-id",
				Version: "order-buildpack-version",
			},
			Order: []builder.OrderEntry{{
				Group: []builder.BuildpackRef{
					{
						BuildpackInfo: bp1v1.Descriptor().Info,
						Optional:      false,
					},
					{
						BuildpackInfo: bp2v1.Descriptor().Info,
						Optional:      false,
					},
				},
			}},
		}}
	})

	it.After(func() {
		baseImage.Cleanup()
		mockController.Finish()
	})

	when("the base image is not valid", func() {
		when("#New", func() {
			when("missing CNB_USER_ID", func() {
				it("returns an error", func() {
					_, err := builder.New(baseImage, "some/builder")
					h.AssertError(t, err, "image 'base/image' missing required env var 'CNB_USER_ID'")
				})
			})

			when("missing CNB_GROUP_ID", func() {
				it.Before(func() {
					h.AssertNil(t, baseImage.SetEnv("CNB_USER_ID", "1234"))
				})

				it("returns an error", func() {
					_, err := builder.New(baseImage, "some/builder")
					h.AssertError(t, err, "image 'base/image' missing required env var 'CNB_GROUP_ID'")
				})
			})

			when("CNB_USER_ID is not an int", func() {
				it.Before(func() {
					h.AssertNil(t, baseImage.SetEnv("CNB_USER_ID", "not an int"))
					h.AssertNil(t, baseImage.SetEnv("CNB_GROUP_ID", "4321"))
				})

				it("returns an error", func() {
					_, err := builder.New(baseImage, "some/builder")
					h.AssertError(t, err, "failed to parse 'CNB_USER_ID', value 'not an int' should be an integer")
				})
			})

			when("CNB_GROUP_ID is not an int", func() {
				it.Before(func() {
					h.AssertNil(t, baseImage.SetEnv("CNB_USER_ID", "1234"))
					h.AssertNil(t, baseImage.SetEnv("CNB_GROUP_ID", "not an int"))
				})

				it("returns an error", func() {
					_, err := builder.New(baseImage, "some/builder")
					h.AssertError(t, err, "failed to parse 'CNB_GROUP_ID', value 'not an int' should be an integer")
				})
			})

			when("missing stack id label", func() {
				it.Before(func() {
					h.AssertNil(t, baseImage.SetEnv("CNB_USER_ID", "1234"))
					h.AssertNil(t, baseImage.SetEnv("CNB_GROUP_ID", "4321"))
				})

				it("returns an error", func() {
					_, err := builder.New(baseImage, "some/builder")
					h.AssertError(t, err, "image 'base/image' missing label 'io.buildpacks.stack.id'")
				})
			})
		})
	})

	when("the base image is a valid build image", func() {
		it.Before(func() {
			var err error
			h.AssertNil(t, baseImage.SetEnv("CNB_USER_ID", "1234"))
			h.AssertNil(t, baseImage.SetEnv("CNB_GROUP_ID", "4321"))
			h.AssertNil(t, baseImage.SetLabel("io.buildpacks.stack.id", "some.stack.id"))
			subject, err = builder.New(baseImage, "some/builder")
			h.AssertNil(t, err)
		})

		it.After(func() {
			baseImage.Cleanup()
		})

		when("#Save", func() {
			var buildpackTgz string

			it.Before(func() {
				buildpackTgz = h.CreateTGZ(t, filepath.Join("testdata", "buildpack"), "./", 0644)
			})

			it.After(func() {
				h.AssertNil(t, os.Remove(buildpackTgz))
			})

			it("creates a builder from the image and renames it", func() {
				h.AssertNil(t, subject.Save())
				h.AssertEq(t, baseImage.IsSaved(), true)
				h.AssertEq(t, baseImage.Name(), "some/builder")
			})

			it("creates the workspace dir with CNB user and group", func() {
				h.AssertNil(t, subject.Save())
				h.AssertEq(t, baseImage.IsSaved(), true)

				layerTar, err := baseImage.FindLayerWithPath("/workspace")
				h.AssertNil(t, err)
				h.AssertOnTarEntry(t, layerTar, "/workspace",
					h.IsDirectory(),
					h.HasFileMode(0755),
					h.HasOwnerAndGroup(1234, 4321),
				)
			})

			it("creates the layers dir with CNB user and group", func() {
				h.AssertNil(t, subject.Save())
				h.AssertEq(t, baseImage.IsSaved(), true)

				layerTar, err := baseImage.FindLayerWithPath("/layers")
				h.AssertNil(t, err)
				h.AssertOnTarEntry(t, layerTar, "/layers",
					h.IsDirectory(),
					h.HasOwnerAndGroup(1234, 4321),
					h.HasFileMode(0755),
				)
			})

			it("creates the buildpacks dir", func() {
				h.AssertNil(t, subject.Save())
				h.AssertEq(t, baseImage.IsSaved(), true)

				layerTar, err := baseImage.FindLayerWithPath("/buildpacks")
				h.AssertNil(t, err)
				h.AssertOnTarEntry(t, layerTar, "/buildpacks",
					h.IsDirectory(),
					h.HasOwnerAndGroup(0, 0),
					h.HasFileMode(0755),
				)
			})

			it("creates the platform dir", func() {
				h.AssertNil(t, subject.Save())
				h.AssertEq(t, baseImage.IsSaved(), true)

				layerTar, err := baseImage.FindLayerWithPath("/platform")
				h.AssertNil(t, err)
				h.AssertOnTarEntry(t, layerTar, "/platform",
					h.IsDirectory(),
					h.HasOwnerAndGroup(0, 0),
					h.HasFileMode(0755),
				)
			})

			it("sets the working dir to the layers dir", func() {
				h.AssertNil(t, subject.Save())
				h.AssertEq(t, baseImage.IsSaved(), true)

				h.AssertEq(t, baseImage.WorkingDir(), "/layers")
			})

			it("does not overwrite the order layer when SetOrder has not been called", func() {
				tmpDir, err := ioutil.TempDir("", "")
				h.AssertNil(t, err)
				defer os.RemoveAll(tmpDir)

				layerFile := filepath.Join(tmpDir, "order.tar")
				f, err := os.Create(layerFile)
				h.AssertNil(t, err)
				defer f.Close()

				err = archive.CreateSingleFileTar(f.Name(), "/buildpacks/order.toml", "some content")
				h.AssertNil(t, err)

				h.AssertNil(t, baseImage.AddLayer(layerFile))
				baseImage.Save()

				h.AssertNil(t, subject.Save())
				h.AssertEq(t, baseImage.IsSaved(), true)

				layerTar, err := baseImage.FindLayerWithPath("/buildpacks/order.toml")
				h.AssertNil(t, err)
				h.AssertOnTarEntry(t, layerTar, "/buildpacks/order.toml", h.ContentEquals("some content"))
			})

			when("validating order", func() {
				when("has single buildpack", func() {
					it.Before(func() {
						subject.AddBuildpack(bp1v1)
					})

					it("should resolve unset version", func() {
						subject.SetOrder(builder.Order{{
							Group: []builder.BuildpackRef{
								{BuildpackInfo: builder.BuildpackInfo{ID: bp1v1.Descriptor().Info.ID}}},
						}})

						err := subject.Save()
						h.AssertNil(t, err)

						label, err := baseImage.Label("io.buildpacks.builder.metadata")
						h.AssertNil(t, err)

						var metadata builder.Metadata
						h.AssertNil(t, json.Unmarshal([]byte(label), &metadata))

						h.AssertEq(t, metadata.Groups[0].Buildpacks[0].ID, "buildpack-1-id")
						h.AssertEq(t, metadata.Groups[0].Buildpacks[0].Version, "buildpack-1-version-1")

						layerTar, err := baseImage.FindLayerWithPath("/buildpacks/order.toml")
						h.AssertNil(t, err)
						h.AssertOnTarEntry(t, layerTar, "/buildpacks/order.toml", h.ContentEquals(`[[order]]

  [[order.group]]
    id = "buildpack-1-id"
    version = "buildpack-1-version-1"
`))
					})

					when("order points to missing buildpack id", func() {
						it("should error", func() {
							subject.SetOrder(builder.Order{{
								Group: []builder.BuildpackRef{
									{BuildpackInfo: builder.BuildpackInfo{ID: "missing-buildpack-id"}}},
							}})

							err := subject.Save()

							h.AssertError(t, err, "no versions of buildpack 'missing-buildpack-id' were found on the builder")
						})
					})

					when("order points to missing buildpack version", func() {
						it("should error", func() {
							subject.SetOrder(builder.Order{{
								Group: []builder.BuildpackRef{
									{BuildpackInfo: builder.BuildpackInfo{ID: "buildpack-1-id", Version: "missing-buildpack-version"}}},
							}})

							err := subject.Save()

							h.AssertError(t, err, "buildpack 'buildpack-1-id' with version 'missing-buildpack-version' was not found on the builder")
						})
					})
				})

				when("has multiple buildpacks with same ID", func() {
					it.Before(func() {
						subject.AddBuildpack(bp1v1)
						subject.AddBuildpack(bp1v2)
					})

					when("order explicitly sets version", func() {
						it("should keep order version", func() {
							subject.SetOrder(builder.Order{{
								Group: []builder.BuildpackRef{
									{BuildpackInfo: bp1v1.Descriptor().Info}},
							}})

							err := subject.Save()
							h.AssertNil(t, err)

							label, err := baseImage.Label("io.buildpacks.builder.metadata")
							h.AssertNil(t, err)

							var metadata builder.Metadata
							h.AssertNil(t, json.Unmarshal([]byte(label), &metadata))

							h.AssertEq(t, metadata.Groups[0].Buildpacks[0].ID, "buildpack-1-id")
							h.AssertEq(t, metadata.Groups[0].Buildpacks[0].Version, "buildpack-1-version-1")

							layerTar, err := baseImage.FindLayerWithPath("/buildpacks/order.toml")
							h.AssertNil(t, err)
							h.AssertOnTarEntry(t, layerTar, "/buildpacks/order.toml", h.ContentEquals(`[[order]]

  [[order.group]]
    id = "buildpack-1-id"
    version = "buildpack-1-version-1"
`))
						})
					})

					when("order version is empty", func() {
						it("return error", func() {
							subject.SetOrder(builder.Order{{
								Group: []builder.BuildpackRef{
									{BuildpackInfo: builder.BuildpackInfo{ID: "buildpack-1-id"}}},
							}})

							err := subject.Save()
							h.AssertError(t, err, "multiple versions of 'buildpack-1-id' - must specify an explicit version")
						})
					})
				})
			})

			when("validating buildpacks", func() {
				when("buildpack is missing both order and stack", func() {
					it("returns an error", func() {
						subject.AddBuildpack(&fakeBuildpack{
							descriptor: builder.BuildpackDescriptor{Info: bp1v1.Descriptor().Info},
						})

						err := subject.Save()

						h.AssertError(t, err, "buildpack 'buildpack-1-id@buildpack-1-version-1' must have either stacks or an order defined")
					})
				})

				when("buildpack has both order and stack", func() {
					it("returns an error", func() {
						subject.AddBuildpack(&fakeBuildpack{descriptor: builder.BuildpackDescriptor{
							Info:   bpOrder.Descriptor().Info,
							Order:  bpOrder.Descriptor().Order,
							Stacks: bp1v1.Descriptor().Stacks,
						}})

						err := subject.Save()

						h.AssertError(t, err, "buildpack 'order-buildpack-id@order-buildpack-version' cannot have both stacks and an order defined")
					})
				})

				when("nested buildpack does not exist", func() {
					when("buildpack by id does not exist", func() {
						it("returns an error", func() {
							subject.AddBuildpack(bp1v1)
							subject.AddBuildpack(bpOrder)

							// order buildpack requires bp2v1
							err := subject.Save()

							h.AssertError(t, err, "buildpack 'buildpack-2-id@buildpack-2-version-1' not found on the builder")
						})
					})

					when("buildpack version does not exist", func() {
						it("returns an error", func() {
							subject.AddBuildpack(bp1v2)
							subject.AddBuildpack(bp2v1)

							// order buildpack requires bp1v1 rather than bp1v2
							subject.AddBuildpack(bpOrder)

							err := subject.Save()

							h.AssertError(t, err, "buildpack 'buildpack-1-id@buildpack-1-version-1' not found on the builder")
						})
					})
				})

				when("buildpack stack id does not match", func() {
					it("returns an error", func() {
						subject.AddBuildpack(&fakeBuildpack{descriptor: builder.BuildpackDescriptor{
							Info:   bp1v1.Descriptor().Info,
							Stacks: []builder.Stack{{ID: "other.stack.id"}},
						}})

						err := subject.Save()

						h.AssertError(t, err, "buildpack 'buildpack-1-id@buildpack-1-version-1' does not support stack 'some.stack.id'")
					})
				})
			})
		})

		when("#SetLifecycle", func() {
			it.Before(func() {
				mockLifecycle.EXPECT().Descriptor().Return(builder.LifecycleDescriptor{
					Info: builder.LifecycleInfo{
						Version: &builder.Version{Version: *semver.MustParse("1.2.3")},
					},
					API: builder.LifecycleAPI{
						PlatformVersion:  "2.2",
						BuildpackVersion: "3.3",
					},
				}).AnyTimes()
				h.AssertNil(t, subject.SetLifecycle(mockLifecycle))

				h.AssertNil(t, subject.Save())
				h.AssertEq(t, baseImage.IsSaved(), true)
			})

			it("should set the lifecycle version successfully", func() {
				h.AssertEq(t, subject.GetLifecycleVersion().String(), "1.2.3")
			})

			it("should add the lifecycle binaries as an image layer", func() {
				layerTar, err := baseImage.FindLayerWithPath("/lifecycle")
				h.AssertNil(t, err)
				h.AssertOnTarEntry(t, layerTar, "/lifecycle",
					h.IsDirectory(),
					h.HasFileMode(0755),
				)

				h.AssertOnTarEntry(t, layerTar, "/lifecycle/detector",
					h.ContentEquals("detector"),
					h.HasFileMode(0755),
				)

				h.AssertOnTarEntry(t, layerTar, "/lifecycle/restorer",
					h.ContentEquals("restorer"),
					h.HasFileMode(0755),
				)

				h.AssertOnTarEntry(t, layerTar, "/lifecycle/analyzer",
					h.ContentEquals("analyzer"),
					h.HasFileMode(0755),
				)

				h.AssertOnTarEntry(t, layerTar, "/lifecycle/builder",
					h.ContentEquals("builder"),
					h.HasFileMode(0755),
				)

				h.AssertOnTarEntry(t, layerTar, "/lifecycle/exporter",
					h.ContentEquals("exporter"),
					h.HasFileMode(0755),
				)

				h.AssertOnTarEntry(t, layerTar, "/lifecycle/cacher",
					h.ContentEquals("cacher"),
					h.HasFileMode(0755),
				)

				h.AssertOnTarEntry(t, layerTar, "/lifecycle/launcher",
					h.ContentEquals("launcher"),
					h.HasFileMode(0755),
				)
			})

			it("sets the lifecycle version on the metadata", func() {
				label, err := baseImage.Label("io.buildpacks.builder.metadata")
				h.AssertNil(t, err)

				var metadata builder.Metadata
				h.AssertNil(t, json.Unmarshal([]byte(label), &metadata))
				h.AssertEq(t, metadata.Lifecycle.Version.String(), "1.2.3")
				h.AssertEq(t, metadata.Lifecycle.API.PlatformVersion, "2.2")
				h.AssertEq(t, metadata.Lifecycle.API.BuildpackVersion, "3.3")
			})
		})

		when("#AddBuildpack", func() {
			it.Before(func() {
				subject.AddBuildpack(bp1v1)

				subject.AddBuildpack(bp1v2)

				subject.AddBuildpack(bp2v1)

				subject.AddBuildpack(bpOrder)
			})

			it("adds the buildpack as an image layer", func() {
				h.AssertNil(t, subject.Save())
				h.AssertEq(t, baseImage.IsSaved(), true)
				assertImageHasBPLayer(t, baseImage, bp1v1)
				assertImageHasBPLayer(t, baseImage, bp1v2)
				assertImageHasBPLayer(t, baseImage, bp2v1)
				assertImageHasBPLayer(t, baseImage, bpOrder)
			})

			when("lifecycle version is < 0.4.0", func() {
				it.Before(func() {
					mockLifecycle.EXPECT().Descriptor().Return(builder.LifecycleDescriptor{
						Info: builder.LifecycleInfo{
							Version: &builder.Version{Version: *semver.MustParse("0.3.0")},
						},
					}).AnyTimes()

					h.AssertNil(t, subject.SetLifecycle(mockLifecycle))
					h.AssertNil(t, subject.Save())
					h.AssertEq(t, baseImage.IsSaved(), true)
				})

				it("adds latest symlinks", func() {
					layerTar, err := baseImage.FindLayerWithPath("/buildpacks/buildpack-2-id/buildpack-2-version-1")
					h.AssertNil(t, err)

					h.AssertOnTarEntry(t, layerTar, "/buildpacks/buildpack-2-id/latest",
						h.SymlinksTo("/buildpacks/buildpack-2-id/buildpack-2-version-1"),
					)
				})
			})

			when("lifecycle version is >= 0.4.0", func() {
				it.Before(func() {
					mockLifecycle.EXPECT().Descriptor().Return(builder.LifecycleDescriptor{
						Info: builder.LifecycleInfo{
							Version: &builder.Version{Version: *semver.MustParse("0.4.0")},
						},
					}).AnyTimes()
					h.AssertNil(t, subject.SetLifecycle(mockLifecycle))

					h.AssertNil(t, subject.Save())
					h.AssertEq(t, baseImage.IsSaved(), true)
				})

				it("doesn't add the latest symlink", func() {
					layerTar, err := baseImage.FindLayerWithPath("/buildpacks/buildpack-2-id/buildpack-2-version-1")
					h.AssertNil(t, err)

					headers, err := h.ListTarContents(layerTar)
					h.AssertNil(t, err)
					for _, header := range headers {
						if strings.Contains(header.Name, "latest") {
							t.Fatalf("found an unexpected latest entry %s", header.Name)
						}
					}
				})
			})

			it("adds the buildpack metadata", func() {
				h.AssertNil(t, subject.Save())
				h.AssertEq(t, baseImage.IsSaved(), true)

				label, err := baseImage.Label("io.buildpacks.builder.metadata")
				h.AssertNil(t, err)

				var metadata builder.Metadata
				h.AssertNil(t, json.Unmarshal([]byte(label), &metadata))
				h.AssertEq(t, len(metadata.Buildpacks), 4)

				h.AssertEq(t, metadata.Buildpacks[0].ID, "buildpack-1-id")
				h.AssertEq(t, metadata.Buildpacks[0].Version, "buildpack-1-version-1")
				h.AssertEq(t, metadata.Buildpacks[0].Latest, false)

				h.AssertEq(t, metadata.Buildpacks[1].ID, "buildpack-1-id")
				h.AssertEq(t, metadata.Buildpacks[1].Version, "buildpack-1-version-2")
				h.AssertEq(t, metadata.Buildpacks[1].Latest, false)

				h.AssertEq(t, metadata.Buildpacks[2].ID, "buildpack-2-id")
				h.AssertEq(t, metadata.Buildpacks[2].Version, "buildpack-2-version-1")
				h.AssertEq(t, metadata.Buildpacks[2].Latest, true)

				h.AssertEq(t, metadata.Buildpacks[3].ID, "order-buildpack-id")
				h.AssertEq(t, metadata.Buildpacks[3].Version, "order-buildpack-version")
				h.AssertEq(t, metadata.Buildpacks[3].Latest, true)
			})

			when("base image already has metadata", func() {
				it.Before(func() {
					h.AssertNil(t, baseImage.SetLabel(
						"io.buildpacks.builder.metadata",
						`{"buildpacks": [{"id": "prev.id"}], "groups": [{"buildpacks": [{"id": "prev.id"}]}], "stack": {"runImage": {"image": "prev/run", "mirrors": ["prev/mirror"]}}, "lifecycle": {"version": "6.6.6"}}`,
					))

					var err error
					subject, err = builder.New(baseImage, "some/builder")
					h.AssertNil(t, err)

					subject.AddBuildpack(bp1v1)
					h.AssertNil(t, subject.Save())
					h.AssertEq(t, baseImage.IsSaved(), true)
				})

				it("appends the buildpack to the metadata", func() {
					label, err := baseImage.Label("io.buildpacks.builder.metadata")
					h.AssertNil(t, err)

					var metadata builder.Metadata
					h.AssertNil(t, json.Unmarshal([]byte(label), &metadata))
					h.AssertEq(t, len(metadata.Buildpacks), 2)

					// keeps original metadata
					h.AssertEq(t, metadata.Buildpacks[0].ID, "prev.id")
					h.AssertEq(t, metadata.Groups[0].Buildpacks[0].ID, "prev.id")
					h.AssertEq(t, metadata.Stack.RunImage.Image, "prev/run")
					h.AssertEq(t, metadata.Stack.RunImage.Mirrors[0], "prev/mirror")
					h.AssertEq(t, subject.GetLifecycleVersion().String(), "6.6.6")

					// adds new buildpack
					h.AssertEq(t, metadata.Buildpacks[1].ID, "buildpack-1-id")
					h.AssertEq(t, metadata.Buildpacks[1].Version, "buildpack-1-version-1")
					h.AssertEq(t, metadata.Buildpacks[1].Latest, true)
				})
			})
		})

		when("#SetOrder", func() {
			when("the buildpacks exist in the image", func() {
				it.Before(func() {
					subject.AddBuildpack(bp1v1)
					subject.AddBuildpack(bp2v1)
					subject.SetOrder(builder.Order{
						{Group: []builder.BuildpackRef{
							{
								BuildpackInfo: bp1v1.Descriptor().Info,
							},
							{
								BuildpackInfo: bp2v1.Descriptor().Info,
								Optional:      true,
							},
						}},
					})

					h.AssertNil(t, subject.Save())
					h.AssertEq(t, baseImage.IsSaved(), true)
				})

				it("adds the order.toml to the image", func() {
					layerTar, err := baseImage.FindLayerWithPath("/buildpacks/order.toml")
					h.AssertNil(t, err)
					h.AssertOnTarEntry(t, layerTar, "/buildpacks/order.toml", h.ContentEquals(`[[order]]

  [[order.group]]
    id = "buildpack-1-id"
    version = "buildpack-1-version-1"

  [[order.group]]
    id = "buildpack-2-id"
    version = "buildpack-2-version-1"
    optional = true
`))
				})

				it("adds the order to the metadata", func() {
					label, err := baseImage.Label("io.buildpacks.builder.metadata")
					h.AssertNil(t, err)

					var metadata builder.Metadata
					h.AssertNil(t, json.Unmarshal([]byte(label), &metadata))

					h.AssertEq(t, len(metadata.Groups), 1)
					h.AssertEq(t, len(metadata.Groups[0].Buildpacks), 2)

					h.AssertEq(t, metadata.Groups[0].Buildpacks[0].ID, "buildpack-1-id")
					h.AssertEq(t, metadata.Groups[0].Buildpacks[0].Version, "buildpack-1-version-1")

					h.AssertEq(t, metadata.Groups[0].Buildpacks[1].ID, "buildpack-2-id")
					h.AssertEq(t, metadata.Groups[0].Buildpacks[1].Version, "buildpack-2-version-1")
					h.AssertEq(t, metadata.Groups[0].Buildpacks[1].Optional, true)
				})
			})
		})

		when("#SetDescription", func() {
			it.Before(func() {
				subject.SetDescription("Some description")
				h.AssertNil(t, subject.Save())
				h.AssertEq(t, baseImage.IsSaved(), true)
			})

			it("sets the description on the metadata", func() {
				label, err := baseImage.Label("io.buildpacks.builder.metadata")
				h.AssertNil(t, err)

				var metadata builder.Metadata
				h.AssertNil(t, json.Unmarshal([]byte(label), &metadata))
				h.AssertEq(t, metadata.Description, "Some description")
			})
		})

		when("#SetStackInfo", func() {
			it.Before(func() {
				subject.SetStackInfo(builder.StackConfig{
					RunImage:        "some/run",
					RunImageMirrors: []string{"some/mirror", "other/mirror"},
				})
				h.AssertNil(t, subject.Save())
				h.AssertEq(t, baseImage.IsSaved(), true)
			})

			it("adds the stack.toml to the image", func() {
				layerTar, err := baseImage.FindLayerWithPath("/buildpacks/stack.toml")
				h.AssertNil(t, err)
				h.AssertOnTarEntry(t, layerTar, "/buildpacks/stack.toml", h.ContentEquals(`[run-image]
  image = "some/run"
  mirrors = ["some/mirror", "other/mirror"]
`))
			})

			it("adds the stack to the metadata", func() {
				label, err := baseImage.Label("io.buildpacks.builder.metadata")
				h.AssertNil(t, err)

				var metadata builder.Metadata
				h.AssertNil(t, json.Unmarshal([]byte(label), &metadata))
				h.AssertEq(t, metadata.Stack.RunImage.Image, "some/run")
				h.AssertEq(t, metadata.Stack.RunImage.Mirrors[0], "some/mirror")
				h.AssertEq(t, metadata.Stack.RunImage.Mirrors[1], "other/mirror")
			})
		})

		when("#SetEnv", func() {
			it.Before(func() {
				subject.SetEnv(map[string]string{
					"SOME_KEY":  "some-val",
					"OTHER_KEY": "other-val",
				})
				h.AssertNil(t, subject.Save())
				h.AssertEq(t, baseImage.IsSaved(), true)
			})

			it("adds the env vars as files to the image", func() {
				layerTar, err := baseImage.FindLayerWithPath("/platform/env/SOME_KEY")
				h.AssertNil(t, err)
				h.AssertOnTarEntry(t, layerTar, "/platform/env/SOME_KEY", h.ContentEquals(`some-val`))
				h.AssertOnTarEntry(t, layerTar, "/platform/env/OTHER_KEY", h.ContentEquals(`other-val`))
			})
		})
	})
}

type fakeBuildpack struct {
	descriptor builder.BuildpackDescriptor
}

func (f *fakeBuildpack) Descriptor() builder.BuildpackDescriptor {
	return f.descriptor
}

func (f *fakeBuildpack) Open() (io.ReadCloser, error) {
	return archive.ReadDirAsTar(filepath.Join("testdata", "buildpack"), ".", 0, 0, -1), nil
}

func assertImageHasBPLayer(t *testing.T, image *fakes.Image, bp builder.Buildpack) {
	dirPath := fmt.Sprintf("/buildpacks/%s/%s", bp.Descriptor().Info.ID, bp.Descriptor().Info.Version)
	layerTar, err := image.FindLayerWithPath(dirPath)
	h.AssertNil(t, err)

	h.AssertOnTarEntry(t, layerTar, dirPath,
		h.IsDirectory(),
	)

	h.AssertOnTarEntry(t, layerTar, dirPath+"/buildpack-file",
		h.ContentEquals("buildpack-contents"),
		h.HasOwnerAndGroup(1234, 4321),
		h.HasFileMode(0644),
	)
}
