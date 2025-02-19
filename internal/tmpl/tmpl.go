// Package tmpl provides templating utilities for goreleaser.
package tmpl

import (
	"bytes"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
	"time"

	"github.com/goreleaser/goreleaser/internal/artifact"
	"github.com/goreleaser/goreleaser/pkg/build"
	"github.com/goreleaser/goreleaser/pkg/context"
)

// Template holds data that can be applied to a template string.
type Template struct {
	fields Fields
}

// Fields that will be available to the template engine.
type Fields map[string]interface{}

const (
	// general keys.
	projectName     = "ProjectName"
	version         = "Version"
	rawVersion      = "RawVersion"
	tag             = "Tag"
	branch          = "Branch"
	commit          = "Commit"
	shortCommit     = "ShortCommit"
	fullCommit      = "FullCommit"
	commitDate      = "CommitDate"
	commitTimestamp = "CommitTimestamp"
	gitURL          = "GitURL"
	major           = "Major"
	minor           = "Minor"
	patch           = "Patch"
	prerelease      = "Prerelease"
	isSnapshot      = "IsSnapshot"
	env             = "Env"
	date            = "Date"
	timestamp       = "Timestamp"

	// artifact-only keys.
	osKey        = "Os"
	arch         = "Arch"
	arm          = "Arm"
	mips         = "Mips"
	binary       = "Binary"
	artifactName = "ArtifactName"
	artifactPath = "ArtifactPath"

	// gitlab only.
	artifactUploadHash = "ArtifactUploadHash"

	// build keys.
	name   = "Name"
	ext    = "Ext"
	path   = "Path"
	target = "Target"
)

// New Template.
func New(ctx *context.Context) *Template {
	sv := ctx.Semver
	rawVersionV := fmt.Sprintf("%d.%d.%d", sv.Major, sv.Minor, sv.Patch)

	return &Template{
		fields: Fields{
			projectName:     ctx.Config.ProjectName,
			version:         ctx.Version,
			rawVersion:      rawVersionV,
			tag:             ctx.Git.CurrentTag,
			branch:          ctx.Git.Branch,
			commit:          ctx.Git.Commit,
			shortCommit:     ctx.Git.ShortCommit,
			fullCommit:      ctx.Git.FullCommit,
			commitDate:      ctx.Git.CommitDate.UTC().Format(time.RFC3339),
			commitTimestamp: ctx.Git.CommitDate.UTC().Unix(),
			gitURL:          ctx.Git.URL,
			env:             ctx.Env,
			date:            ctx.Date.UTC().Format(time.RFC3339),
			timestamp:       ctx.Date.UTC().Unix(),
			major:           ctx.Semver.Major,
			minor:           ctx.Semver.Minor,
			patch:           ctx.Semver.Patch,
			prerelease:      ctx.Semver.Prerelease,
			isSnapshot:      ctx.Snapshot,
		},
	}
}

// WithEnvS overrides template's env field with the given KEY=VALUE list of
// environment variables.
func (t *Template) WithEnvS(envs []string) *Template {
	var result = map[string]string{}
	for _, env := range envs {
		var parts = strings.SplitN(env, "=", 2)
		result[parts[0]] = parts[1]
	}
	return t.WithEnv(result)
}

// WithEnv overrides template's env field with the given environment map.
func (t *Template) WithEnv(e map[string]string) *Template {
	t.fields[env] = e
	return t
}

// WithExtraFields allows to add new more custom fields to the template.
// It will override fields with the same name.
func (t *Template) WithExtraFields(f Fields) *Template {
	for k, v := range f {
		t.fields[k] = v
	}
	return t
}

// WithArtifact populates Fields from the artifact and replacements.
func (t *Template) WithArtifact(a *artifact.Artifact, replacements map[string]string) *Template {
	var bin = a.Extra[binary]
	if bin == nil {
		bin = t.fields[projectName]
	}
	t.fields[osKey] = replace(replacements, a.Goos)
	t.fields[arch] = replace(replacements, a.Goarch)
	t.fields[arm] = replace(replacements, a.Goarm)
	t.fields[mips] = replace(replacements, a.Gomips)
	t.fields[binary] = bin.(string)
	t.fields[artifactName] = a.Name
	t.fields[artifactPath] = a.Path
	if val, ok := a.Extra["ArtifactUploadHash"]; ok {
		t.fields[artifactUploadHash] = val
	} else {
		t.fields[artifactUploadHash] = ""
	}
	return t
}

func (t *Template) WithBuildOptions(opts build.Options) *Template {
	return t.WithExtraFields(buildOptsToFields(opts))
}

func buildOptsToFields(opts build.Options) Fields {
	return Fields{
		target: opts.Target,
		ext:    opts.Ext,
		name:   opts.Name,
		path:   opts.Path,
		osKey:  opts.Os,
		arch:   opts.Arch,
	}
}

// Apply applies the given string against the Fields stored in the template.
func (t *Template) Apply(s string) (string, error) {
	var out bytes.Buffer
	tmpl, err := template.New("tmpl").
		Option("missingkey=error").
		Funcs(template.FuncMap{
			"replace": strings.ReplaceAll,
			"time": func(s string) string {
				return time.Now().UTC().Format(s)
			},
			"tolower":    strings.ToLower,
			"toupper":    strings.ToUpper,
			"trim":       strings.TrimSpace,
			"trimprefix": strings.TrimPrefix,
			"dir":        filepath.Dir,
			"abs":        filepath.Abs,
		}).
		Parse(s)
	if err != nil {
		return "", err
	}

	err = tmpl.Execute(&out, t.fields)
	return out.String(), err
}

type ExpectedSingleEnvErr struct{}

func (e ExpectedSingleEnvErr) Error() string {
	return "expected {{ .Env.VAR_NAME }} only (no plain-text or other interpolation)"
}

// ApplySingleEnvOnly enforces template to only contain a single environment variable
// and nothing else.
func (t *Template) ApplySingleEnvOnly(s string) (string, error) {
	s = strings.TrimSpace(s)
	if len(s) == 0 {
		return "", nil
	}

	// text/template/parse (lexer) could be used here too,
	// but regexp reduces the complexity and should be sufficient,
	// given the context is mostly discouraging users from bad practice
	// of hard-coded credentials, rather than catch all possible cases
	envOnlyRe := regexp.MustCompile(`^{{\s*\.Env\.[^.\s}]+\s*}}$`)
	if !envOnlyRe.Match([]byte(s)) {
		return "", ExpectedSingleEnvErr{}
	}

	var out bytes.Buffer
	tmpl, err := template.New("tmpl").
		Option("missingkey=error").
		Parse(s)
	if err != nil {
		return "", err
	}

	err = tmpl.Execute(&out, t.fields)
	return out.String(), err
}

func replace(replacements map[string]string, original string) string {
	result := replacements[original]
	if result == "" {
		return original
	}
	return result
}
