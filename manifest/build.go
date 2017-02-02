package manifest

import (
	"bufio"
	"bytes"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"
)

type BuildOptions struct {
	Cache       bool
	Environment map[string]string
	Service     string
}

func (m *Manifest) Build(dir, appName string, s Stream, opts BuildOptions) error {
	pulls := map[string][]string{}
	builds := []Service{}

	services, err := m.runOrder(opts.Service)
	if err != nil {
		return err
	}

	for _, service := range services {
		dockerFile := service.Build.Dockerfile
		if dockerFile == "" {
			dockerFile = service.Dockerfile
		}
		if image := service.Image; image != "" {
			// make the implicit :latest explicit for caching/pulling
			sp := strings.Split(image, "/")
			if !strings.Contains(sp[len(sp)-1], ":") {
				image = image + ":latest"
			}
			pulls[image] = append(pulls[image], service.Tag(appName))
		} else {
			builds = append(builds, service)
		}
	}

	buildCache := map[string]string{}

	for _, service := range builds {
		if bc, ok := buildCache[service.Build.Hash()]; ok {
			if err := DefaultRunner.Run(s, Docker("tag", bc, service.Tag(appName))); err != nil {
				return fmt.Errorf("build error: %s", err)
			}
			continue
		}

		args := []string{"build"}

		if !opts.Cache {
			args = append(args, "--no-cache")
		}

		context := filepath.Join(dir, coalesce(service.Build.Context, "."))
		fmt.Printf("context = %+v\n", context)
		dockerFile := coalesce(service.Dockerfile, "Dockerfile")
		dockerFile = coalesce(service.Build.Dockerfile, dockerFile)
		dockerFile = filepath.Join(context, dockerFile)

		fmt.Printf("dockerFile = %+v\n", dockerFile)
		bargs, err := buildArgs(dockerFile)
		if err != nil {
			return err
		}

		for _, ba := range bargs {
			if v, ok := opts.Environment[ba]; ok {
				args = append(args, "--build-arg", fmt.Sprintf("%s=%q", ba, v))
			}
		}

		args = append(args, "-f", dockerFile)
		args = append(args, "-t", service.Tag(appName))
		args = append(args, context)

		if err := DefaultRunner.Run(s, Docker(args...)); err != nil {
			return fmt.Errorf("build error: %s", err)
		}

		buildCache[service.Build.Hash()] = service.Tag(appName)
	}

	for image, tags := range pulls {
		args := []string{"pull"}

		output, err := DefaultRunner.CombinedOutput(Docker("images", "-q", image))
		if err != nil {
			return err
		}

		args = append(args, image)

		if !opts.Cache || len(output) == 0 {
			if err := DefaultRunner.Run(s, Docker("pull", image)); err != nil {
				return fmt.Errorf("build error: %s", err)
			}
		}
		for _, tag := range tags {
			if err := DefaultRunner.Run(s, Docker("tag", image, tag)); err != nil {
				return fmt.Errorf("build error: %s", err)
			}
		}
	}

	return nil
}

func buildArgs(dockerfile string) ([]string, error) {
	args := []string{}

	data, err := ioutil.ReadFile(dockerfile)
	if err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))

	for scanner.Scan() {
		parts := strings.Fields(scanner.Text())

		if len(parts) < 1 {
			continue
		}

		switch parts[0] {
		case "ARG":
			args = append(args, strings.SplitN(parts[1], "=", 2)[0])
		}
	}

	return args, nil
}
