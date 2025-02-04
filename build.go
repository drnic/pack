package pack

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/buildpack/imgutil"
	"github.com/docker/docker/api/types"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/pkg/errors"

	"github.com/buildpack/pack/build"
	"github.com/buildpack/pack/builder"
	"github.com/buildpack/pack/buildpack"
	"github.com/buildpack/pack/internal/archive"
	"github.com/buildpack/pack/style"
)

type Lifecycle interface {
	Execute(ctx context.Context, opts build.LifecycleOptions) error
}

type BuildOptions struct {
	Image             string              // required
	Builder           string              // required
	AppPath           string              // defaults to current working directory
	RunImage          string              // defaults to the best mirror from the builder metadata or AdditionalMirrors
	AdditionalMirrors map[string][]string // only considered if RunImage is not provided
	Env               map[string]string
	Publish           bool
	NoPull            bool
	ClearCache        bool
	Buildpacks        []string
	ProxyConfig       *ProxyConfig // defaults to  environment proxy vars
}

type ProxyConfig struct {
	HTTPProxy  string
	HTTPSProxy string
	NoProxy    string
}

func (c *Client) Build(ctx context.Context, opts BuildOptions) error {
	imageRef, err := c.parseTagReference(opts.Image)
	if err != nil {
		return errors.Wrapf(err, "invalid image name '%s'", opts.Image)
	}

	appPath, err := c.processAppPath(opts.AppPath)
	if err != nil {
		return errors.Wrapf(err, "invalid app path '%s'", opts.AppPath)
	}

	proxyConfig := c.processProxyConfig(opts.ProxyConfig)

	builderRef, err := c.processBuilderName(opts.Builder)
	if err != nil {
		return errors.Wrapf(err, "invalid builder '%s'", opts.Builder)
	}

	rawBuilderImage, err := c.imageFetcher.Fetch(ctx, builderRef.Name(), true, !opts.NoPull)
	if err != nil {
		return errors.Wrapf(err, "failed to fetch builder image '%s'", builderRef.Name())
	}

	builderImage, err := c.processBuilderImage(rawBuilderImage)
	if err != nil {
		return errors.Wrapf(err, "invalid builder '%s'", opts.Builder)
	}

	runImage := c.resolveRunImage(opts.RunImage, imageRef.Context().RegistryStr(), builderImage.GetStackInfo(), opts.AdditionalMirrors)

	if _, err := c.validateRunImage(ctx, runImage, opts.NoPull, opts.Publish, builderImage.StackID); err != nil {
		return errors.Wrapf(err, "invalid run-image '%s'", runImage)
	}

	extraBuildpacks, group, err := c.processBuildpacks(opts.Buildpacks)
	if err != nil {
		return errors.Wrap(err, "invalid buildpack")
	}

	ephemeralBuilder, err := c.createEphemeralBuilder(rawBuilderImage, opts.Env, group, extraBuildpacks)
	if err != nil {
		return err
	}
	defer c.docker.ImageRemove(context.Background(), ephemeralBuilder.Name(), types.ImageRemoveOptions{Force: true})

	return c.lifecycle.Execute(ctx, build.LifecycleOptions{
		AppPath:    appPath,
		Image:      imageRef,
		Builder:    ephemeralBuilder,
		RunImage:   runImage,
		ClearCache: opts.ClearCache,
		Publish:    opts.Publish,
		HTTPProxy:  proxyConfig.HTTPProxy,
		HTTPSProxy: proxyConfig.HTTPSProxy,
		NoProxy:    proxyConfig.NoProxy,
	})
}

func (c *Client) processBuilderName(builderName string) (name.Reference, error) {
	if builderName == "" {
		return nil, errors.New("builder is a required parameter if the client has no default builder")
	}
	return name.ParseReference(builderName, name.WeakValidation)
}

func (c *Client) processBuilderImage(img imgutil.Image) (*builder.Builder, error) {
	builder, err := builder.GetBuilder(img)
	if err != nil {
		return nil, err
	}
	if builder.GetStackInfo().RunImage.Image == "" {
		return nil, errors.New("builder metadata is missing runImage")
	}
	return builder, nil
}

func (c *Client) validateRunImage(context context.Context, name string, noPull bool, publish bool, expectedStack string) (imgutil.Image, error) {
	if name == "" {
		return nil, errors.New("run image must be specified")
	}
	img, err := c.imageFetcher.Fetch(context, name, !publish, !noPull)
	if err != nil {
		return nil, err
	}
	stackID, err := img.Label("io.buildpacks.stack.id")
	if err != nil {
		return nil, err
	}
	if stackID != expectedStack {
		return nil, fmt.Errorf("run-image stack id '%s' does not match builder stack '%s'", stackID, expectedStack)
	}
	return img, nil
}

func (c *Client) processAppPath(appPath string) (string, error) {
	var (
		resolvedAppPath = appPath
		err             error
	)

	if appPath == "" {
		if appPath, err = os.Getwd(); err != nil {
			return "", errors.Wrap(err, "get working dir")
		}
	}

	if resolvedAppPath, err = filepath.EvalSymlinks(appPath); err != nil {
		return "", errors.Wrap(err, "evaluate symlink")
	}

	if resolvedAppPath, err = filepath.Abs(resolvedAppPath); err != nil {
		return "", errors.Wrap(err, "resolve absolute path")
	}

	fi, err := os.Stat(resolvedAppPath)
	if err != nil {
		return "", errors.Wrap(err, "stat file")
	}

	if !fi.IsDir() {
		fh, err := os.Open(resolvedAppPath)
		if err != nil {
			return "", errors.Wrap(err, "read file")
		}
		defer fh.Close()

		isZip, err := archive.IsZip(fh)
		if err != nil {
			return "", errors.Wrap(err, "check zip")
		}

		if !isZip {
			return "", errors.New("app path must be a directory or zip")
		}
	}

	return resolvedAppPath, nil
}

func (c *Client) processProxyConfig(config *ProxyConfig) ProxyConfig {
	var (
		httpProxy, httpsProxy, noProxy string
		ok                             bool
	)
	if config != nil {
		return *config
	}
	if httpProxy, ok = os.LookupEnv("HTTP_PROXY"); !ok {
		httpProxy = os.Getenv("http_proxy")
	}
	if httpsProxy, ok = os.LookupEnv("HTTPS_PROXY"); !ok {
		httpsProxy = os.Getenv("https_proxy")
	}
	if noProxy, ok = os.LookupEnv("NO_PROXY"); !ok {
		noProxy = os.Getenv("no_proxy")
	}
	return ProxyConfig{
		HTTPProxy:  httpProxy,
		HTTPSProxy: httpsProxy,
		NoProxy:    noProxy,
	}
}

func (c *Client) processBuildpacks(buildpacks []string) ([]buildpack.Buildpack, builder.OrderEntry, error) {
	group := builder.OrderEntry{Group: []builder.BuildpackRef{}}
	var bps []buildpack.Buildpack
	for _, bp := range buildpacks {
		if isBuildpackId(bp) {
			id, version := c.parseBuildpack(bp)
			group.Group = append(group.Group, builder.BuildpackRef{
				BuildpackInfo: buildpack.BuildpackInfo{
					ID:      id,
					Version: version,
				},
			})
		} else {
			if runtime.GOOS == "windows" && filepath.Ext(bp) != ".tgz" {
				return nil, builder.OrderEntry{}, fmt.Errorf("buildpack %s: Windows only supports .tgz-based buildpacks", style.Symbol(bp))
			}
			c.logger.Debugf("fetching buildpack from %s", style.Symbol(bp))
			fetchedBP, err := c.buildpackFetcher.FetchBuildpack(bp)
			if err != nil {
				return nil, builder.OrderEntry{}, errors.Wrapf(err, "failed to fetch buildpack from URI '%s'", bp)
			}
			bps = append(bps, fetchedBP)
			group.Group = append(group.Group, builder.BuildpackRef{
				BuildpackInfo: fetchedBP.BuildpackInfo,
			})
		}
	}
	return bps, group, nil
}

func isBuildpackId(path string) bool {
	if _, err := os.Stat(filepath.Join(path, "buildpack.toml")); err == nil {
		return false
	}

	if !schemeRegexp.MatchString(path) {
		if _, err := os.Stat(path); err != nil {
			return true
		}
	}

	return false
}

func (c *Client) parseBuildpack(bp string) (string, string) {
	parts := strings.Split(bp, "@")
	if len(parts) == 2 {
		if parts[1] == "latest" {
			c.logger.Warn("@latest syntax is deprecated, will not work in future releases")
			return parts[0], ""
		}

		return parts[0], parts[1]
	}

	return parts[0], ""
}

func (c *Client) createEphemeralBuilder(rawBuilderImage imgutil.Image, env map[string]string, group builder.OrderEntry, buildpacks []buildpack.Buildpack) (*builder.Builder, error) {
	origBuilderName := rawBuilderImage.Name()
	bldr, err := builder.New(rawBuilderImage, fmt.Sprintf("pack.local/builder/%x:latest", randString(10)))
	if err != nil {
		return nil, errors.Wrapf(err, "invalid builder %s", style.Symbol(origBuilderName))
	}
	bldr.SetEnv(env)
	for _, bp := range buildpacks {
		c.logger.Debugf("adding buildpack %s version %s to builder", style.Symbol(bp.ID), style.Symbol(bp.Version))
		bldr.AddBuildpack(bp)
	}
	if len(group.Group) > 0 {
		c.logger.Debug("setting custom order")
		bldr.SetOrder([]builder.OrderEntry{group})
	}
	if err := bldr.Save(); err != nil {
		return nil, err
	}
	return bldr, nil
}

func randString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a' + byte(rand.Intn(26))
	}
	return string(b)
}
