// +build acceptance

package acceptance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/buildpack/pack/style"

	"github.com/Masterminds/semver"
	"github.com/buildpack/lifecycle/metadata"
	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/pkg/errors"
	"github.com/sclevine/spec"
	"github.com/sclevine/spec/report"

	"github.com/buildpack/pack/cache"
	"github.com/buildpack/pack/internal/archive"
	"github.com/buildpack/pack/lifecycle"
	h "github.com/buildpack/pack/testhelpers"
)

const (
	envPackPath              = "PACK_PATH"
	envPreviousPackPath      = "PREVIOUS_PACK_PATH"
	envLifecyclePath         = "LIFECYCLE_PATH"
	envPreviousLifecyclePath = "PREVIOUS_LIFECYCLE_PATH"
	envAcceptanceSuiteConfig = "ACCEPTANCE_SUITE_CONFIG"

	runImage   = "pack-test/run"
	buildImage = "pack-test/build"
)

var (
	packHome       string
	dockerCli      *client.Client
	registryConfig *h.TestRegistryConfig

	lifecycleV020 = semver.MustParse("0.2.0")
	lifecycleV030 = semver.MustParse("0.3.0")
)

func TestAcceptance(t *testing.T) {
	var err error

	h.RequireDocker(t)
	rand.Seed(time.Now().UTC().UnixNano())

	dockerCli, err = client.NewClientWithOpts(client.FromEnv, client.WithVersion("1.38"))
	h.AssertNil(t, err)

	registryConfig = h.RunRegistry(t, false)
	defer registryConfig.StopRegistry(t)

	runImageMirror := registryConfig.RepoName(runImage)
	createStack(t, dockerCli, runImageMirror)
	defer h.DockerRmi(dockerCli, runImage, buildImage, runImageMirror)

	suite := spec.New("acceptance suite", spec.Report(report.Terminal{}))

	packPath := os.Getenv(envPackPath)
	if packPath == "" {
		packPath = buildPack(t, "../cmd/pack")
	}

	previousPackPath := os.Getenv(envPreviousPackPath)

	lifecycleVersion := *semver.MustParse(lifecycle.DefaultLifecycleVersion)
	lifecyclePath := os.Getenv(envLifecyclePath)
	if lifecyclePath != "" {
		lifecyclePath, err = filepath.Abs(lifecyclePath)
		if err != nil {
			t.Fatal(err)
		}

		lifecycleVersion, err = extractLifecycleVersion(lifecyclePath)
		if err != nil {
			t.Fatal(err)
		}
	}

	previousLifecycleVersion := lifecycleVersion
	previousLifecyclePath := os.Getenv(envPreviousLifecyclePath)
	if previousLifecyclePath != "" {
		previousLifecyclePath, err = filepath.Abs(previousLifecyclePath)
		if err != nil {
			t.Fatal(err)
		}

		previousLifecycleVersion, err = extractLifecycleVersion(previousLifecyclePath)
		if err != nil {
			t.Fatal(err)
		}
	}

	combos := []runCombo{
		{Pack: "current", PackCreateBuilder: "current", Lifecycle: "current"},
	}

	suiteConfig := os.Getenv(envAcceptanceSuiteConfig)
	if suiteConfig != "" {
		combos, err = parseSuiteConfig(suiteConfig)
		h.AssertNil(t, err)
	}

	resolvedCombos, err := resolveRunCombinations(combos, packPath, previousPackPath, lifecyclePath, lifecycleVersion, previousLifecyclePath, previousLifecycleVersion)
	h.AssertNil(t, err)

	for k, combo := range resolvedCombos {
		t.Logf("setting up run combination %s as %+v", style.Symbol(k), combo)

		builder := createBuilder(t, runImageMirror, combo.builderTomlPath, combo.packCreateBuilderPath, combo.lifecyclePath, combo.lifecycleVersion)
		//noinspection ALL
		defer h.DockerRmi(dockerCli, builder)

		combo := combo
		suite(k, func(t *testing.T, when spec.G, it spec.S) {
			testAcceptance(t, when, it, builder, runImageMirror, combo.packFixturesDir, combo.packPath, combo.lifecycleVersion)
		}, spec.Report(report.Terminal{}))
	}

	suite.Run(t)
}

func testAcceptance(t *testing.T, when spec.G, it spec.S, builder, runImageMirror, packFixturesDir, packPath string, lifecycleVersion semver.Version) {
	var packCmd = func(name string, args ...string) *exec.Cmd {
		cmdArgs := append([]string{
			name,
			"--no-color",
		}, args...)
		cmd := exec.Command(
			packPath,
			cmdArgs...,
		)
		cmd.Env = append(os.Environ(), "PACK_HOME="+packHome, "DOCKER_CONFIG="+registryConfig.DockerConfigDir)
		return cmd
	}

	it.Before(func() {
		var err error
		packHome, err = ioutil.TempDir("", "buildpack.pack.home.")
		h.AssertNil(t, err)
	})

	when("invalid subcommand", func() {
		it("prints usage", func() {
			cmd := packCmd("some-bad-command")
			output, _ := cmd.CombinedOutput()
			if !strings.Contains(string(output), `unknown command "some-bad-command" for "pack"`) {
				t.Fatal("Failed to print usage", string(output))
			}
			if !strings.Contains(string(output), `Run 'pack --help' for usage.`) {
				t.Fatal("Failed to print usage", string(output))
			}
		})
	})

	when("build", func() {
		var repo, repoName, containerName string

		it.Before(func() {
			repo = "some-org/" + h.RandString(10)
			repoName = registryConfig.RepoName(repo)
			containerName = "test-" + h.RandString(10)
		})

		it.After(func() {
			dockerCli.ContainerKill(context.TODO(), containerName, "SIGKILL")
			dockerCli.ContainerRemove(context.TODO(), containerName, dockertypes.ContainerRemoveOptions{Force: true})
			dockerCli.ImageRemove(context.TODO(), repoName, dockertypes.ImageRemoveOptions{Force: true, PruneChildren: true})
			ref, err := name.ParseReference(repoName, name.WeakValidation)
			h.AssertNil(t, err)
			cacheImage := cache.NewImageCache(ref, dockerCli)
			buildCacheVolume := cache.NewVolumeCache(ref, "build", dockerCli)
			launchCacheVolume := cache.NewVolumeCache(ref, "launch", dockerCli)
			cacheImage.Clear(context.TODO())
			buildCacheVolume.Clear(context.TODO())
			launchCacheVolume.Clear(context.TODO())
		})

		when("default builder is set", func() {
			it.Before(func() {
				h.Run(t, packCmd("set-default-builder", builder))
			})

			it("creates a runnable, rebuildable image on daemon from app dir", func() {
				appPath := filepath.Join("testdata", "mock_app")
				cmd := packCmd(
					"build", repoName,
					"-p", appPath,
				)
				output := h.Run(t, cmd)
				h.AssertContains(t, output, fmt.Sprintf("Successfully built image '%s'", repoName))
				imgId, err := imgIdFromOutput(output, repoName, lifecycleVersion)
				if err != nil {
					t.Log(output)
					t.Fatal("Could not determine image id for built image")
				}
				defer h.DockerRmi(dockerCli, imgId)

				if lifecycleVersion.GreaterThan(lifecycleV020) || lifecycleVersion.Equal(lifecycleV020) {
					t.Log("uses a build cache volume when appropriate")
					h.AssertContains(t, output, "Using build cache volume")
				} else {
					t.Log("uses a build cache image when appropriate")
					h.AssertContains(t, output, "Using build cache image")
				}

				t.Log("app is runnable")
				assertMockAppRunsWithOutput(t, repoName, "Launch Dep Contents", "Cached Dep Contents")

				t.Log("selects the best run image mirror")
				h.AssertContains(t, output, fmt.Sprintf("Selected run image mirror '%s'", runImageMirror))

				t.Log("it uses the run image as a base image")
				assertHasBase(t, repoName, runImage)

				t.Log("sets the run image metadata")
				runImageLabel := imageLabel(t, dockerCli, repoName, metadata.AppMetadataLabel)
				h.AssertContains(t, runImageLabel, fmt.Sprintf(`"stack":{"runImage":{"image":"%s","mirrors":["%s"]}}}`, runImage, runImageMirror))

				t.Log("registry is empty")
				contents, err := registryConfig.RegistryCatalog()
				h.AssertNil(t, err)
				if strings.Contains(contents, repo) {
					t.Fatalf("Should not have published image without the '--publish' flag: got %s", contents)
				}

				t.Log("add a local mirror")
				localRunImageMirror := registryConfig.RepoName("pack-test/run-mirror")
				h.AssertNil(t, dockerCli.ImageTag(context.TODO(), runImage, localRunImageMirror))
				defer h.DockerRmi(dockerCli, localRunImageMirror)
				cmd = packCmd("set-run-image-mirrors", runImage, "-m", localRunImageMirror)
				h.Run(t, cmd)

				t.Log("rebuild")
				cmd = packCmd("build", repoName, "-p", appPath)
				output = h.Run(t, cmd)
				h.AssertContains(t, output, fmt.Sprintf("Successfully built image '%s'", repoName))
				imgId, err = imgIdFromOutput(output, repoName, lifecycleVersion)
				if err != nil {
					t.Log(output)
					t.Fatal("Could not determine image id for built image")
				}
				defer h.DockerRmi(dockerCli, imgId)

				t.Log("local run-image mirror is selected")
				h.AssertContains(t, output, fmt.Sprintf("Selected run image mirror '%s' from local config", localRunImageMirror))

				t.Log("app is runnable")
				assertMockAppRunsWithOutput(t, repoName, "Launch Dep Contents", "Cached Dep Contents")

				t.Log("restores the cache")
				h.AssertContainsMatch(t, output, `(?i)\[restorer] restoring cached layer 'simple/layers:cached-launch-layer'`)
				h.AssertContainsMatch(t, output, `(?i)\[analyzer] using cached launch layer 'simple/layers:cached-launch-layer'`)

				t.Log("exporter and cacher reuse unchanged layers")
				h.AssertContainsMatch(t, output, `(?i)\[exporter] reusing layer 'simple/layers:cached-launch-layer'`)
				h.AssertContainsMatch(t, output, `(?i)\[cacher] reusing layer 'simple/layers:cached-launch-layer'`)

				t.Log("rebuild with --clear-cache")
				cmd = packCmd("build", repoName, "-p", appPath, "--clear-cache")
				output = h.Run(t, cmd)
				h.AssertContains(t, output, fmt.Sprintf("Successfully built image '%s'", repoName))

				t.Log("skips restore")
				h.AssertContains(t, output, "Skipping 'restore' due to clearing cache")

				if lifecycleVersion.GreaterThan(lifecycleV030) || lifecycleVersion.Equal(lifecycleV030) {
					t.Log("skips buildpack layer analysis")
					h.AssertContainsMatch(t, output, `(?i)\[analyzer] Skipping buildpack layer analysis`)

					t.Log("exporter reuses unchanged layers")
					h.AssertContainsMatch(t, output, `(?i)\[exporter] reusing layer 'simple/layers:cached-launch-layer'`)
				}

				t.Log("cacher adds layers")
				h.AssertContainsMatch(t, output, `(?i)\[cacher] (Caching|adding) layer 'simple/layers:cached-launch-layer'`)
			})

			it("supports building app from a zip file", func() {
				appPath := filepath.Join("testdata", "mock_app.zip")
				cmd := packCmd(
					"build", repoName,
					"-p", appPath,
				)
				output := h.Run(t, cmd)
				h.AssertContains(t, output, fmt.Sprintf("Successfully built image '%s'", repoName))
				imgId, err := imgIdFromOutput(output, repoName, lifecycleVersion)
				if err != nil {
					t.Log(output)
					t.Fatal("Could not determine image id for built image")
				}
				defer h.DockerRmi(dockerCli, imgId)
			})

			when("--buildpack", func() {
				when("the argument is a tgz or id", func() {
					var notBuilderTgz string

					it.Before(func() {
						notBuilderTgz = h.CreateTgz(t, filepath.Join(buildpacksDir(lifecycleVersion), "not-in-builder-buildpack"), "./", 0766)
					})

					it.After(func() {
						h.AssertNil(t, os.Remove(notBuilderTgz))
					})

					it("adds the buildpacks to the builder if necessary and runs them", func() {
						cmd := packCmd(
							"build", repoName,
							"-p", filepath.Join("testdata", "mock_app"),
							"--buildpack", notBuilderTgz, // tgz not in builder
							"--buildpack", "simple/layers@simple-layers-version", // with version
							"--buildpack", "noop.buildpack", // without version
							"--buildpack", "read/env@latest", // latest (for backwards compatibility)
							"--env", "DETECT_ENV_BUILDPACK=true",
						)
						output := h.Run(t, cmd)
						h.AssertContains(t, output, "NOOP Buildpack")
						h.AssertContains(t, output, "Read Env Buildpack")
						h.AssertContains(t, output, fmt.Sprintf("Successfully built image '%s'", repoName))

						t.Log("app is runnable")
						assertMockAppRunsWithOutput(t, repoName,
							"Local Buildpack Dep Contents",
							"Launch Dep Contents",
							"Cached Dep Contents",
						)
					})
				})

				when("the argument is directory", func() {
					it("adds the buildpacks to the builder if necessary and runs them", func() {
						h.SkipIf(t, runtime.GOOS == "windows", "buildpack directories not supported on windows")

						cmd := packCmd(
							"build", repoName,
							"-p", filepath.Join("testdata", "mock_app"),
							"--buildpack", filepath.Join(buildpacksDir(lifecycleVersion), "not-in-builder-buildpack"),
						)
						output := h.Run(t, cmd)
						h.AssertContains(t, output, fmt.Sprintf("Successfully built image '%s'", repoName))
						t.Log("app is runnable")
						assertMockAppRunsWithOutput(t, repoName, "Local Buildpack Dep Contents")
					})
				})

				when("the buildpack stack doesn't match the builder", func() {
					var otherStackBuilderTgz string

					it.Before(func() {
						otherStackBuilderTgz = h.CreateTgz(t, filepath.Join(buildpacksDir(lifecycleVersion), "other-stack-buildpack"), "./", 0766)
					})

					it.After(func() {
						h.AssertNil(t, os.Remove(otherStackBuilderTgz))
					})

					it("errors", func() {
						cmd := packCmd(
							"build", repoName,
							"-p", filepath.Join("testdata", "mock_app"),
							"--buildpack", otherStackBuilderTgz,
						)
						txt, err := h.RunE(cmd)
						h.AssertNotNil(t, err)
						h.AssertContains(t, txt, "other/stack/bp")
						h.AssertContains(t, txt, "other-stack-version")
						h.AssertContains(t, txt, "does not support stack 'pack.test.stack'")
					})
				})
			})

			when("--env-file", func() {
				var envPath string

				it.Before(func() {
					envfile, err := ioutil.TempFile("", "envfile")
					h.AssertNil(t, err)
					defer envfile.Close()

					err = os.Setenv("ENV2_CONTENTS", "Env2 Layer Contents From Environment")
					h.AssertNil(t, err)
					envfile.WriteString(`
            DETECT_ENV_BUILDPACK="true"
			ENV1_CONTENTS="Env1 Layer Contents From File"
			ENV2_CONTENTS
			`)
					envPath = envfile.Name()
				})

				it.After(func() {
					h.AssertNil(t, os.Unsetenv("ENV2_CONTENTS"))
					h.AssertNil(t, os.RemoveAll(envPath))
				})

				it("provides the env vars to the build and detect steps", func() {
					cmd := packCmd(
						"build", repoName,
						"-p", filepath.Join("testdata", "mock_app"),
						"--env-file", envPath,
					)
					output := h.Run(t, cmd)
					h.AssertContains(t, output, fmt.Sprintf("Successfully built image '%s'", repoName))
					assertMockAppRunsWithOutput(t,
						repoName,
						"Env2 Layer Contents From Environment",
						"Env1 Layer Contents From File",
					)
				})
			})

			when("--env", func() {
				it.Before(func() {
					h.AssertNil(t,
						os.Setenv("ENV2_CONTENTS", "Env2 Layer Contents From Environment"),
					)
				})

				it.After(func() {
					h.AssertNil(t, os.Unsetenv("ENV2_CONTENTS"))
				})

				it("provides the env vars to the build and detect steps", func() {
					cmd := packCmd(
						"build", repoName,
						"-p", filepath.Join("testdata", "mock_app"),
						"--env", "DETECT_ENV_BUILDPACK=true",
						"--env", `ENV1_CONTENTS="Env1 Layer Contents From Command Line"`,
						"--env", "ENV2_CONTENTS",
					)
					output := h.Run(t, cmd)
					h.AssertContains(t, output, fmt.Sprintf("Successfully built image '%s'", repoName))
					assertMockAppRunsWithOutput(t, repoName, "Env2 Layer Contents From Environment", "Env1 Layer Contents From Command Line")
				})
			})

			when("--run-image", func() {
				var runImageName string

				when("the run-image has the correct stack ID", func() {
					it.Before(func() {
						runImageName = h.CreateImageOnRemote(t, dockerCli, registryConfig, "custom-run-image"+h.RandString(10), fmt.Sprintf(`
													FROM %s
													USER root
													RUN echo "custom-run" > /custom-run.txt
													USER pack
												`, runImage))
					})

					it.After(func() {
						h.DockerRmi(dockerCli, runImageName)
					})

					it("uses the run image as the base image", func() {
						cmd := packCmd(
							"build", repoName,
							"-p", filepath.Join("testdata", "mock_app"),
							"--run-image", runImageName,
						)

						output := h.Run(t, cmd)
						h.AssertContains(t, output, fmt.Sprintf("Successfully built image '%s'", repoName))

						t.Log("app is runnable")
						assertMockAppRunsWithOutput(t, repoName, "Launch Dep Contents", "Cached Dep Contents")

						t.Log("pulls the run image")
						h.AssertContains(t, output, fmt.Sprintf("Pulling image '%s'", runImageName))

						t.Log("uses the run image as the base image")
						assertHasBase(t, repoName, runImageName)
					})
				})

				when("the run image has the wrong stack ID", func() {
					it.Before(func() {
						runImageName = h.CreateImageOnRemote(t, dockerCli, registryConfig, "custom-run-image"+h.RandString(10), fmt.Sprintf(`
													FROM %s
													LABEL io.buildpacks.stack.id=other.stack.id
													USER pack
												`, runImage))

					})

					it.After(func() {
						h.DockerRmi(dockerCli, runImageName)
					})

					it("fails with a message", func() {
						cmd := packCmd(
							"build", repoName,
							"-p", filepath.Join("testdata", "mock_app"),
							"--run-image", runImageName,
						)
						txt, err := h.RunE(cmd)
						h.AssertNotNil(t, err)
						h.AssertContains(t, txt, "run-image stack id 'other.stack.id' does not match builder stack 'pack.test.stack'")
					})
				})
			})

			when("--publish", func() {
				it("creates image on the registry", func() {
					runPackBuild := func() string {
						t.Helper()
						cmd := packCmd(
							"build", repoName,
							"-p", filepath.Join("testdata", "mock_app"),
							"--publish",
						)
						return h.Run(t, cmd)
					}
					output := runPackBuild()
					h.AssertContains(t, output, fmt.Sprintf("Successfully built image '%s'", repoName))
					imgDigest, err := imgDigestFromOutput(output, repoName, lifecycleVersion)
					if err != nil {
						t.Log(output)
						t.Fatal("Could not determine sha for built image")
					}

					t.Log("checking that registry has contents")
					contents, err := registryConfig.RegistryCatalog()
					h.AssertNil(t, err)
					if !strings.Contains(contents, repo) {
						t.Fatalf("Expected to see image %s in %s", repo, contents)
					}

					h.AssertNil(t, h.PullImageWithAuth(dockerCli, fmt.Sprintf("%s@%s", repoName, imgDigest), registryConfig.RegistryAuth()))
					defer h.DockerRmi(dockerCli, fmt.Sprintf("%s@%s", repoName, imgDigest))

					t.Log("app is runnable")
					assertMockAppRunsWithOutput(t, fmt.Sprintf("%s@%s", repoName, imgDigest), "Launch Dep Contents", "Cached Dep Contents")
				})
			})

			when("ctrl+c", func() {
				it("stops the execution", func() {
					var buf bytes.Buffer
					cmd := packCmd("build", repoName, "-p", filepath.Join("testdata", "mock_app"))
					cmd.Stdout = &buf
					cmd.Stderr = &buf

					h.AssertNil(t, cmd.Start())

					go terminateAtStep(t, cmd, &buf, "[detector]")

					err := cmd.Wait()
					h.AssertNotNil(t, err)
					h.AssertNotContains(t, buf.String(), "Successfully built image")
				})
			})
		})

		when("default builder is not set", func() {
			it("informs the user", func() {
				cmd := packCmd("build", repoName, "-p", filepath.Join("testdata", "mock_app"))
				output, err := h.RunE(cmd)
				h.AssertNotNil(t, err)
				h.AssertContains(t, output, `Please select a default builder with:`)
				h.AssertMatch(t, output, `Cloud Foundry:\s+'cloudfoundry/cnb:bionic'`)
				h.AssertMatch(t, output, `Cloud Foundry:\s+'cloudfoundry/cnb:cflinuxfs3'`)
				h.AssertMatch(t, output, `Heroku:\s+'heroku/buildpacks:18'`)
			})
		})
	})

	when("run", func() {
		it.Before(func() {
			h.SkipIf(t, runtime.GOOS == "windows", "Skipping because windows fails to clean up properly")
		})

		when("there is a builder", func() {
			var (
				listeningPort string
				err           error
			)

			it.Before(func() {
				listeningPort, err = h.GetFreePort()
				h.AssertNil(t, err)
			})

			it.After(func() {
				absPath, err := filepath.Abs(filepath.Join("testdata", "mock_app"))
				h.AssertNil(t, err)

				sum := sha256.Sum256([]byte(absPath))
				repoName := fmt.Sprintf("pack.local/run/%x", sum[:8])
				ref, err := name.ParseReference(repoName, name.WeakValidation)
				h.AssertNil(t, err)

				h.DockerRmi(dockerCli, repoName)

				cache.NewImageCache(ref, dockerCli).Clear(context.TODO())
				cache.NewVolumeCache(ref, "build", dockerCli).Clear(context.TODO())
				cache.NewVolumeCache(ref, "launch", dockerCli).Clear(context.TODO())
			})

			it("starts an image", func() {
				var buf bytes.Buffer
				cmd := packCmd("run",
					"--port", listeningPort+":8080",
					"-p", filepath.Join("testdata", "mock_app"),
					"--builder", builder,
				)
				cmd.Stdout = &buf
				cmd.Stderr = &buf
				h.AssertNil(t, cmd.Start())

				defer ctrlCProc(cmd)

				h.AssertEq(t, isCommandRunning(cmd), true)
				assertMockAppResponseContains(t, listeningPort, 30*time.Second, "Launch Dep Contents", "Cached Dep Contents")
			})
		})

		when("default builder is not set", func() {
			it("informs the user", func() {
				cmd := packCmd("run", "-p", filepath.Join("testdata", "mock_app"))
				output, err := h.RunE(cmd)
				h.AssertNotNil(t, err)
				h.AssertContains(t, output, `Please select a default builder with:`)
				h.AssertMatch(t, output, `Cloud Foundry:\s+'cloudfoundry/cnb:bionic'`)
				h.AssertMatch(t, output, `Cloud Foundry:\s+'cloudfoundry/cnb:cflinuxfs3'`)
				h.AssertMatch(t, output, `Heroku:\s+'heroku/buildpacks:18'`)
			})
		})
	})

	when("rebase", func() {
		var repoName, runBefore, origID string
		var buildRunImage func(string, string, string)

		it.Before(func() {
			repoName = registryConfig.RepoName("some-org/" + h.RandString(10))
			runBefore = registryConfig.RepoName("run-before/" + h.RandString(10))

			buildRunImage = func(newRunImage, contents1, contents2 string) {
				h.CreateImageOnLocal(t, dockerCli, newRunImage, fmt.Sprintf(`
													FROM %s
													USER root
													RUN echo %s > /contents1.txt
													RUN echo %s > /contents2.txt
													USER pack
												`, runImage, contents1, contents2))
			}
			buildRunImage(runBefore, "contents-before-1", "contents-before-2")

			cmd := packCmd(
				"build", repoName,
				"-p", filepath.Join("testdata", "mock_app"),
				"--builder", builder,
				"--run-image", runBefore,
				"--no-pull",
			)
			h.Run(t, cmd)
			origID = h.ImageID(t, repoName)
			assertMockAppRunsWithOutput(t, repoName, "contents-before-1", "contents-before-2")
		})

		it.After(func() {
			h.AssertNil(t, h.DockerRmi(dockerCli, origID, repoName, runBefore))
			ref, err := name.ParseReference(repoName, name.WeakValidation)
			h.AssertNil(t, err)
			buildCacheVolume := cache.NewVolumeCache(ref, "build", dockerCli)
			launchCacheVolume := cache.NewVolumeCache(ref, "launch", dockerCli)
			h.AssertNil(t, buildCacheVolume.Clear(context.TODO()))
			h.AssertNil(t, launchCacheVolume.Clear(context.TODO()))
		})

		when("daemon", func() {
			when("--run-image", func() {
				var runAfter string

				it.Before(func() {
					runAfter = registryConfig.RepoName("run-after/" + h.RandString(10))
					buildRunImage(runAfter, "contents-after-1", "contents-after-2")
				})

				it.After(func() {
					h.AssertNil(t, h.DockerRmi(dockerCli, runAfter))
				})

				it("uses provided run image", func() {
					cmd := packCmd("rebase", repoName, "--no-pull", "--run-image", runAfter)
					output := h.Run(t, cmd)

					h.AssertContains(t, output, fmt.Sprintf("Successfully rebased image '%s'", repoName))
					assertMockAppRunsWithOutput(t, repoName, "contents-after-1", "contents-after-2")
				})
			})

			when("local config has a mirror", func() {
				var localRunImageMirror string

				it.Before(func() {
					localRunImageMirror = registryConfig.RepoName("run-after/" + h.RandString(10))
					buildRunImage(localRunImageMirror, "local-mirror-after-1", "local-mirror-after-2")
					cmd := packCmd("set-run-image-mirrors", runImage, "-m", localRunImageMirror)
					h.Run(t, cmd)
				})

				it.After(func() {
					h.AssertNil(t, h.DockerRmi(dockerCli, localRunImageMirror))
				})

				it("prefers the local mirror", func() {
					cmd := packCmd("rebase", repoName, "--no-pull")
					output := h.Run(t, cmd)

					h.AssertContains(t, output, fmt.Sprintf("Selected run image mirror '%s' from local config", localRunImageMirror))

					h.AssertContains(t, output, fmt.Sprintf("Successfully rebased image '%s'", repoName))
					assertMockAppRunsWithOutput(t, repoName, "local-mirror-after-1", "local-mirror-after-2")
				})
			})

			when("image metadata has a mirror", func() {
				it.Before(func() {
					// clean up existing mirror first to avoid leaking images
					h.AssertNil(t, h.DockerRmi(dockerCli, runImageMirror))

					buildRunImage(runImageMirror, "mirror-after-1", "mirror-after-2")
				})

				it("selects the best mirror", func() {
					cmd := packCmd("rebase", repoName, "--no-pull")
					output := h.Run(t, cmd)

					h.AssertContains(t, output, fmt.Sprintf("Selected run image mirror '%s'", runImageMirror))

					h.AssertContains(t, output, fmt.Sprintf("Successfully rebased image '%s'", repoName))
					assertMockAppRunsWithOutput(t, repoName, "mirror-after-1", "mirror-after-2")
				})
			})
		})

		when("--publish", func() {
			it.Before(func() {
				h.AssertNil(t, h.PushImage(dockerCli, repoName, registryConfig))
			})

			when("--run-image", func() {
				var runAfter string

				it.Before(func() {
					runAfter = registryConfig.RepoName("run-after/" + h.RandString(10))
					buildRunImage(runAfter, "contents-after-1", "contents-after-2")
					h.AssertNil(t, h.PushImage(dockerCli, runAfter, registryConfig))
				})

				it.After(func() {
					h.DockerRmi(dockerCli, runAfter)
				})

				it("uses provided run image", func() {
					cmd := packCmd("rebase", repoName, "--publish", "--run-image", runAfter)
					output := h.Run(t, cmd)

					h.AssertContains(t, output, fmt.Sprintf("Successfully rebased image '%s'", repoName))
					h.AssertNil(t, h.PullImageWithAuth(dockerCli, repoName, registryConfig.RegistryAuth()))
					assertMockAppRunsWithOutput(t, repoName, "contents-after-1", "contents-after-2")
				})
			})
		})
	})

	when("suggest-builders", func() {
		it("displays suggested builders", func() {
			cmd := packCmd("suggest-builders")
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("suggest-builders command failed: %s: %s", output, err)
			}
			h.AssertContains(t, string(output), "Suggested builders:")
			h.AssertContains(t, string(output), "cloudfoundry/cnb:bionic")
		})
	})

	when("suggest-stacks", func() {
		it("displays suggested stacks", func() {
			cmd := packCmd("suggest-stacks")
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("suggest-stacks command failed: %s: %s", output, err)
			}
			h.AssertContains(t, string(output), "Stacks maintained by the Cloud Native Buildpacks project:")
			h.AssertContains(t, string(output), "Stacks maintained by the community:")
		})
	})

	when("set-default-builder", func() {
		it("sets the default-stack-id in ~/.pack/config.toml", func() {
			cmd := packCmd("set-default-builder", "cloudfoundry/cnb:bionic")
			cmd.Env = append(os.Environ(), "PACK_HOME="+packHome)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("set-default-builder command failed: %s: %s", output, err)
			}
			h.AssertContains(t, string(output), "Builder 'cloudfoundry/cnb:bionic' is now the default builder")
		})
	})

	when("inspect-builder", func() {
		it("displays configuration for a builder (local and remote)", func() {
			configuredRunImage := "some-registry.com/pack-test/run1"
			cmd := packCmd("set-run-image-mirrors", "pack-test/run", "--mirror", configuredRunImage)
			output := h.Run(t, cmd)
			h.AssertEq(t, output, "Run Image 'pack-test/run' configured with mirror 'some-registry.com/pack-test/run1'\n")

			cmd = packCmd("inspect-builder", builder)
			output = h.Run(t, cmd)

			expected, err := ioutil.ReadFile(filepath.Join(packFixturesDir, "inspect_builder_output.txt"))
			h.AssertNil(t, err)
			h.AssertEq(t, output, fmt.Sprintf(string(expected), builder, &lifecycleVersion, runImageMirror, &lifecycleVersion, runImageMirror))
		})
	})
}

type runCombo struct {
	Pack              string `json:"pack"`
	PackCreateBuilder string `json:"pack_create_builder"`
	Lifecycle         string `json:"lifecycle"`
}

type resolvedRunCombo struct {
	builderTomlPath       string
	packFixturesDir       string
	packPath              string
	packCreateBuilderPath string
	lifecyclePath         string
	lifecycleVersion      semver.Version
}

func resolveRunCombinations(
	combos []runCombo,
	packPath string,
	previousPackPath string,
	lifecyclePath string,
	lifecycleVersion semver.Version,
	previousLifecyclePath string,
	previousLifecycleVersion semver.Version,
) (map[string]resolvedRunCombo, error) {
	resolved := map[string]resolvedRunCombo{}
	for _, c := range combos {
		key := fmt.Sprintf("p_%s cb_%s lc_%s", c.Pack, c.PackCreateBuilder, c.Lifecycle)
		rc := resolvedRunCombo{
			builderTomlPath:       filepath.Join("testdata", "pack_current", "builder.toml"),
			packFixturesDir:       filepath.Join("testdata", "pack_current"),
			packPath:              packPath,
			packCreateBuilderPath: packPath,
			lifecyclePath:         lifecyclePath,
			lifecycleVersion:      lifecycleVersion,
		}

		if c.Pack == "previous" {
			if previousPackPath == "" {
				return resolved, errors.Errorf("must provide %s in order to run combination %s", style.Symbol(envPreviousPackPath), style.Symbol(key))
			}

			rc.packPath = previousPackPath
			rc.packFixturesDir = filepath.Join("testdata", "pack_previous")
		}

		if c.PackCreateBuilder == "previous" {
			if previousPackPath == "" {
				return resolved, errors.Errorf("must provide %s in order to run combination %s", style.Symbol(envPreviousPackPath), style.Symbol(key))
			}

			rc.packCreateBuilderPath = previousPackPath
			rc.builderTomlPath = filepath.Join("testdata", "pack_previous", "builder.toml")
		}

		if c.Lifecycle == "previous" {
			if previousLifecyclePath == "" {
				return resolved, errors.Errorf("must provide %s in order to run combination %s", style.Symbol(envPreviousLifecyclePath), style.Symbol(key))
			}

			rc.lifecyclePath = previousLifecyclePath
			rc.lifecycleVersion = previousLifecycleVersion
		}

		resolved[key] = rc
	}

	return resolved, nil
}

func parseSuiteConfig(config string) ([]runCombo, error) {
	var cfgs []runCombo
	if err := json.Unmarshal([]byte(config), &cfgs); err != nil {
		return nil, errors.Wrap(err, "parse config")
	}

	validate := func(jsonKey, value string) error {
		switch value {
		case "current", "previous":
			return nil
		default:
			return fmt.Errorf("invalid config: %s not valid value for %s", style.Symbol(value), style.Symbol(jsonKey))
		}
	}

	for _, c := range cfgs {
		if err := validate("pack", c.Pack); err != nil {
			return nil, err
		}

		if err := validate("pack_create_builder", c.PackCreateBuilder); err != nil {
			return nil, err
		}

		if err := validate("lifecycle", c.Lifecycle); err != nil {
			return nil, err
		}
	}

	return cfgs, nil
}

func extractLifecycleVersion(lcPath string) (semver.Version, error) {
	headers, err := h.ListTarContents(lcPath)
	if err != nil {
		return semver.Version{}, err
	}

	regex := regexp.MustCompile(`lifecycle-v?([0-9]+\.[0-9]+\.[0-9]+.*)[\-+]linux`)
	for _, header := range headers {
		matches := regex.FindStringSubmatch(path.Clean(header.Name))
		if matches != nil {
			version, err := semver.NewVersion(matches[1])
			if err != nil {
				return semver.Version{}, errors.Wrapf(err, "parsing version %s", style.Symbol(matches[1]))
			}

			return *version, nil
		}
	}

	return semver.Version{}, fmt.Errorf("could not determine version of %s", style.Symbol(lcPath))
}

func buildpacksDir(lcVersion semver.Version) string {
	d := "api1"
	if lcVersion.GreaterThan(lifecycleV030) {
		d = "api2"
	}
	return filepath.Join("testdata", "mock_buildpacks", d)
}

func buildPack(t *testing.T, packCmdPath string) string {
	packTmpDir, err := ioutil.TempDir("", "pack.acceptance.binary.")
	if err != nil {
		t.Fatal(err)
	}

	packPath := filepath.Join(packTmpDir, "pack")
	if runtime.GOOS == "windows" {
		packPath = packPath + ".exe"
	}

	if txt, err := exec.Command("go", "build", "-o", packPath, packCmdPath).CombinedOutput(); err != nil {
		t.Fatal("building pack cli:\n", string(txt), err)
	}

	return packPath
}

func createBuilder(t *testing.T, runImageMirror, builderTOMLPath, packPath, lifecyclePath string, lifecycleVersion semver.Version) string {
	t.Log("creating builder image...")

	// CREATE TEMP WORKING DIR
	tmpDir, err := ioutil.TempDir("", "create-test-builder")
	h.AssertNil(t, err)
	defer os.RemoveAll(tmpDir)

	// DETERMINE TEST DATA
	buildpacksDir := buildpacksDir(lifecycleVersion)
	t.Log("using buildpacks from: ", buildpacksDir)
	h.RecursiveCopy(t, buildpacksDir, tmpDir)

	// AMEND builder.toml
	h.CopyFile(t, builderTOMLPath, filepath.Join(tmpDir, "builder.toml"))
	builderConfigFile, err := os.OpenFile(filepath.Join(tmpDir, "builder.toml"), os.O_RDWR|os.O_APPEND, 0666)
	h.AssertNil(t, err)

	// ADD run-image-mirrors
	_, err = builderConfigFile.Write([]byte(fmt.Sprintf("run-image-mirrors = [\"%s\"]\n", runImageMirror)))
	h.AssertNil(t, err)

	// ADD lifecycle
	_, err = builderConfigFile.Write([]byte("[lifecycle]\n"))
	h.AssertNil(t, err)

	if lifecyclePath != "" {
		t.Logf("adding lifecycle path '%s' to builder config", lifecyclePath)
		_, err = builderConfigFile.Write([]byte(fmt.Sprintf("uri = \"%s\"\n", strings.ReplaceAll(lifecyclePath, `\`, `\\`))))
		h.AssertNil(t, err)
	}

	t.Logf("adding lifecycle version '%s' to builder config", &lifecycleVersion)
	_, err = builderConfigFile.Write([]byte(fmt.Sprintf("version = \"%s\"\n", &lifecycleVersion)))
	h.AssertNil(t, err)

	builderConfigFile.Close()

	// PACKAGE BUILDPACKS
	buildpacks := []string{
		"noop-buildpack",
		"not-in-builder-buildpack",
		"other-stack-buildpack",
		"read-env-buildpack",
		"simple-layers-buildpack",
	}

	for _, v := range buildpacks {
		tgz := h.CreateTgz(t, filepath.Join(buildpacksDir, v), "./", 0766)
		err := os.Rename(tgz, filepath.Join(tmpDir, v+".tgz"))
		h.AssertNil(t, err)
	}

	// NAME BUILDER
	builder := registryConfig.RepoName("some-org/" + h.RandString(10))

	// CREATE BUILDER
	cmd := exec.Command(packPath, "create-builder", "--no-color", builder, "-b", filepath.Join(tmpDir, "builder.toml"))
	output := h.Run(t, cmd)
	h.AssertContains(t, output, fmt.Sprintf("Successfully created builder image '%s'", builder))
	h.AssertNil(t, h.PushImage(dockerCli, builder, registryConfig))

	return builder
}

func createStack(t *testing.T, dockerCli *client.Client, runImageMirror string) {
	t.Log("creating stack images...")
	createStackImage(t, dockerCli, runImage, filepath.Join("testdata", "mock_stack"))
	h.AssertNil(t, dockerCli.ImageTag(context.Background(), runImage, buildImage))
	h.AssertNil(t, dockerCli.ImageTag(context.Background(), runImage, runImageMirror))
	h.AssertNil(t, h.PushImage(dockerCli, runImageMirror, registryConfig))
}

func createStackImage(t *testing.T, dockerCli *client.Client, repoName string, dir string) {
	ctx := context.Background()
	buildContext := archive.ReadDirAsTar(dir, "/", 0, 0, -1)

	res, err := dockerCli.ImageBuild(ctx, buildContext, dockertypes.ImageBuildOptions{
		Tags:        []string{repoName},
		Remove:      true,
		ForceRemove: true,
	})
	h.AssertNil(t, err)

	io.Copy(ioutil.Discard, res.Body)
	res.Body.Close()
}

func assertMockAppRunsWithOutput(t *testing.T, repoName string, expectedOutputs ...string) {
	t.Helper()
	containerName := "test-" + h.RandString(10)
	runDockerImageExposePort(t, containerName, repoName)
	defer dockerCli.ContainerKill(context.TODO(), containerName, "SIGKILL")
	defer dockerCli.ContainerRemove(context.TODO(), containerName, dockertypes.ContainerRemoveOptions{Force: true})
	launchPort := fetchHostPort(t, containerName)
	assertMockAppResponseContains(t, launchPort, 10*time.Second, expectedOutputs...)
}

func assertMockAppResponseContains(t *testing.T, launchPort string, timeout time.Duration, expectedOutputs ...string) {
	t.Helper()
	resp := waitForResponse(t, launchPort, timeout)
	for _, expected := range expectedOutputs {
		h.AssertContains(t, resp, expected)
	}
}

func assertHasBase(t *testing.T, image, base string) {
	imageInspect, _, err := dockerCli.ImageInspectWithRaw(context.Background(), image)
	h.AssertNil(t, err)
	baseInspect, _, err := dockerCli.ImageInspectWithRaw(context.Background(), base)
	h.AssertNil(t, err)
	for i, layer := range baseInspect.RootFS.Layers {
		h.AssertEq(t, imageInspect.RootFS.Layers[i], layer)
	}
}

func fetchHostPort(t *testing.T, dockerID string) string {
	t.Helper()

	i, err := dockerCli.ContainerInspect(context.Background(), dockerID)
	h.AssertNil(t, err)
	for _, port := range i.NetworkSettings.Ports {
		for _, binding := range port {
			return binding.HostPort
		}
	}

	t.Fatalf("Failed to fetch host port for %s: no ports exposed", dockerID)
	return ""
}

func imgDigestFromOutput(txt, repoName string, lifecycleVersion semver.Version) (string, error) {
	if lifecycleVersion.LessThan(lifecycleV030) {
		for _, m := range regexp.MustCompile(`\*\*\* Image: (.+)@(.+)`).FindAllStringSubmatch(txt, -1) {
			if m[1] == repoName || m[1] == repoName+":latest" {
				return m[2], nil
			}
		}
	}

	for _, m := range regexp.MustCompile(`\*\*\* Digest: (.+)`).FindAllStringSubmatch(txt, -1) {
		return m[1], nil
	}

	return "", errors.New("could not find digest in output")
}

func imgIdFromOutput(txt, repoName string, lifecycleVersion semver.Version) (string, error) {
	if lifecycleVersion.LessThan(lifecycleV030) {
		return imgDigestFromOutput(txt, repoName, lifecycleVersion)
	}

	for _, m := range regexp.MustCompile(`\*\*\* Image ID: (.+)`).FindAllStringSubmatch(txt, -1) {
		return m[1], nil
	}

	return "", errors.New("could not find image ID in output")
}

func runDockerImageExposePort(t *testing.T, containerName, repoName string) string {
	t.Helper()
	ctx := context.Background()

	ctr, err := dockerCli.ContainerCreate(ctx, &container.Config{
		Image:        repoName,
		Env:          []string{"PORT=8080"},
		ExposedPorts: map[nat.Port]struct{}{"8080/tcp": {}},
		Healthcheck:  nil,
	}, &container.HostConfig{
		PortBindings: nat.PortMap{
			"8080/tcp": []nat.PortBinding{{}},
		},
		AutoRemove: true,
	}, nil, containerName)
	h.AssertNil(t, err)

	err = dockerCli.ContainerStart(ctx, ctr.ID, dockertypes.ContainerStartOptions{})
	h.AssertNil(t, err)
	return ctr.ID
}

func waitForResponse(t *testing.T, port string, timeout time.Duration) string {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case <-ticker.C:
			resp, err := h.HttpGetE("http://localhost:"+port, map[string]string{})
			if err != nil {
				break
			}
			return resp
		case <-timer.C:
			t.Fatalf("timeout waiting for response: %v", timeout)
		}
	}
}

func ctrlCProc(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil || cmd.Process.Pid <= 0 {
		return fmt.Errorf("invalid pid: %#v", cmd)
	}
	if runtime.GOOS == "windows" {
		return exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprint(cmd.Process.Pid)).Run()
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		return err
	}
	_, err := cmd.Process.Wait()
	return err
}

func isCommandRunning(cmd *exec.Cmd) bool {
	_, err := os.FindProcess(cmd.Process.Pid)
	if err != nil {
		return false
	}
	return true
}

// FIXME : buf needs a mutex
func terminateAtStep(t *testing.T, cmd *exec.Cmd, buf *bytes.Buffer, pattern string) {
	t.Helper()
	var interruptSignal os.Signal

	if runtime.GOOS == "windows" {
		// Windows does not support os.Interrupt
		interruptSignal = os.Kill
	} else {
		interruptSignal = os.Interrupt
	}

	for {
		if strings.Contains(buf.String(), pattern) {
			h.AssertNil(t, cmd.Process.Signal(interruptSignal))
			return
		}
	}
}

func imageLabel(t *testing.T, dockerCli *client.Client, repoName, labelName string) string {
	t.Helper()
	inspect, _, err := dockerCli.ImageInspectWithRaw(context.Background(), repoName)
	h.AssertNil(t, err)
	label, ok := inspect.Config.Labels[labelName]
	if !ok {
		t.Errorf("expected label %s to exist", labelName)
	}
	return label
}
