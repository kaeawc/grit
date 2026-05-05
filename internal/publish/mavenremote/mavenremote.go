// Package mavenremote implements a Layer 5 publish adapter that uploads
// Maven-layout artifacts to a remote HTTP repository.
package mavenremote

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/kaeawc/grit/internal/cas"
	"github.com/kaeawc/grit/internal/lockfile"
	"github.com/kaeawc/grit/internal/publish"
)

const (
	// DefaultID is the stable identifier for the remote Maven publisher.
	DefaultID = "maven-remote"
	userAgent = "grit-mavenpublish/1"
)

// ErrOffline indicates PublishPin was called on a publisher configured with
// WithOffline(true). No network request is attempted.
var ErrOffline = errors.New("mavenremote publish: offline mode")

// Publisher uploads artifacts to a Maven2-layout HTTP endpoint.
type Publisher struct {
	baseURL    *url.URL
	httpClient *http.Client
	id         string
	headers    map[string]string
	envHeaders []headerEnv
	offline    bool
}

type headerEnv struct {
	header string
	envVar string
}

// Option configures a Publisher at construction.
type Option func(*Publisher)

// WithHTTPClient overrides the http.Client used for requests.
func WithHTTPClient(hc *http.Client) Option {
	return func(p *Publisher) {
		if hc != nil {
			p.httpClient = hc
		}
	}
}

// WithID overrides the publisher identifier.
func WithID(id string) Option {
	return func(p *Publisher) {
		if id != "" {
			p.id = id
		}
	}
}

// WithHeaders supplies static headers that are added to every upload request.
func WithHeaders(headers map[string]string) Option {
	return func(p *Publisher) {
		if len(headers) == 0 {
			return
		}
		out := make(map[string]string, len(headers))
		for k, v := range headers {
			out[k] = v
		}
		p.headers = out
	}
}

// WithEnvHeader adds a request header whose value is read from an
// environment variable at PublishPin time.
func WithEnvHeader(header, envVar string) Option {
	return func(p *Publisher) {
		if header == "" || envVar == "" {
			return
		}
		p.envHeaders = append(p.envHeaders, headerEnv{header: header, envVar: envVar})
	}
}

// WithOffline configures the publisher to refuse every PublishPin call with
// ErrOffline.
func WithOffline(offline bool) Option {
	return func(p *Publisher) { p.offline = offline }
}

// New returns a Publisher rooted at baseURL.
func New(baseURL string, opts ...Option) (*Publisher, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("mavenremote publish: empty baseURL")
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("mavenremote publish: parse baseURL: %w", err)
	}
	if !u.IsAbs() {
		return nil, fmt.Errorf("mavenremote publish: baseURL must be absolute, got %q", baseURL)
	}
	p := &Publisher{
		baseURL:    u,
		httpClient: http.DefaultClient,
		id:         DefaultID,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p, nil
}

// BaseURL returns the repository base URL this publisher targets.
func (p *Publisher) BaseURL() string { return p.baseURL.String() }

// ID implements publish.Publisher.
func (p *Publisher) ID() string { return p.id }

// PublishPin uploads every file named in pin plus Maven checksum sidecars.
// When the pin does not already carry a POM or Gradle module file, PublishPin
// also uploads deterministic generated fallbacks for those files.
func (p *Publisher) PublishPin(ctx context.Context, pin lockfile.Pin, store cas.Store) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.offline {
		return fmt.Errorf("%w: cannot publish %s", ErrOffline, pin.Coordinate)
	}
	for _, file := range pin.Files {
		if err := p.publishBlob(ctx, store, pin, file); err != nil {
			return err
		}
	}
	if !hasPinFileKind(pin, lockfile.FileKindPOM) {
		payload, ok, err := generatedPomPayload(pin)
		if err != nil {
			return err
		}
		if ok {
			if err := p.publishBytes(ctx, pomFileName(pin.Coordinate), pin.Coordinate, payload); err != nil {
				return err
			}
		}
	}
	if !hasPinFileKind(pin, lockfile.FileKindModule) {
		payload, ok, err := generatedModulePayload(pin)
		if err != nil {
			return err
		}
		if ok {
			if err := p.publishBytes(ctx, moduleFileName(pin.Coordinate), pin.Coordinate, payload); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *Publisher) publishBlob(ctx context.Context, store cas.Store, pin lockfile.Pin, file lockfile.PinFile) error {
	rc, err := store.Get(ctx, file.Hash)
	if err != nil {
		return fmt.Errorf("mavenremote publish %s: get blob %s: %w", pin.Coordinate, file.Hash, err)
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(rc)
	if err != nil {
		return fmt.Errorf("mavenremote publish %s: read blob %s: %w", pin.Coordinate, file.Hash, err)
	}
	return p.publishBytes(ctx, file.Name, pin.Coordinate, data)
}

func (p *Publisher) publishBytes(ctx context.Context, name string, coord lockfile.Coordinate, data []byte) error {
	target := p.targetURL(coord, name)
	if err := p.putBytes(ctx, target, data); err != nil {
		return err
	}
	sha1Digest := sha1.Sum(data)
	if err := p.putBytes(ctx, target+".sha1", []byte(hex.EncodeToString(sha1Digest[:]))); err != nil {
		return err
	}
	md5Digest := md5.Sum(data)
	if err := p.putBytes(ctx, target+".md5", []byte(hex.EncodeToString(md5Digest[:]))); err != nil {
		return err
	}
	return nil
}

func (p *Publisher) putBytes(ctx context.Context, target string, data []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, target, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("mavenremote publish: build request: %w", err)
	}
	p.applyRequestHeaders(req.Header)
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("mavenremote publish: PUT %s: %w", redactURLUserinfo(target), err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusNoContent:
		return nil
	default:
		return fmt.Errorf("mavenremote publish: PUT %s: %s", redactURLUserinfo(target), resp.Status)
	}
}

func (p *Publisher) targetURL(coord lockfile.Coordinate, name string) string {
	base := *p.baseURL
	base.Path = path.Join(base.Path, groupURLPath(coord.Group), coord.Artifact, coord.Version, name)
	return base.String()
}

func (p *Publisher) applyRequestHeaders(headers http.Header) {
	headers.Set("User-Agent", userAgent)
	for _, binding := range p.envHeaders {
		if value, ok := os.LookupEnv(binding.envVar); ok && value != "" {
			headers.Set(binding.header, value)
		}
	}
	for k, v := range p.headers {
		headers.Set(k, v)
	}
}

func redactURLUserinfo(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	u.User = nil
	return u.String()
}

func groupURLPath(group string) string {
	return strings.ReplaceAll(group, ".", "/")
}

func pomFileName(coord lockfile.Coordinate) string {
	return coord.Artifact + "-" + coord.Version + ".pom"
}

func moduleFileName(coord lockfile.Coordinate) string {
	return coord.Artifact + "-" + coord.Version + ".module"
}

func hasPinFileKind(pin lockfile.Pin, kind lockfile.FileKind) bool {
	for _, file := range pin.Files {
		if file.Kind == kind {
			return true
		}
	}
	return false
}

type mavenPomProject struct {
	XMLName      xml.Name             `xml:"project"`
	Xmlns        string               `xml:"xmlns,attr,omitempty"`
	Xsi          string               `xml:"xmlns:xsi,attr,omitempty"`
	Schema       string               `xml:"xsi:schemaLocation,attr,omitempty"`
	Model        string               `xml:"modelVersion"`
	GroupID      string               `xml:"groupId"`
	ArtifactID   string               `xml:"artifactId"`
	Version      string               `xml:"version"`
	Packaging    string               `xml:"packaging,omitempty"`
	Dependencies []mavenPomDependency `xml:"dependencies>dependency,omitempty"`
}

type mavenPomDependency struct {
	GroupID    string `xml:"groupId"`
	ArtifactID string `xml:"artifactId"`
	Version    string `xml:"version"`
	Scope      string `xml:"scope,omitempty"`
}

func generatedPomPayload(pin lockfile.Pin) ([]byte, bool, error) {
	if pin.Coordinate.Group == "" || pin.Coordinate.Artifact == "" || pin.Coordinate.Version == "" {
		return nil, false, fmt.Errorf("incomplete coordinate: %+v", pin.Coordinate)
	}
	deps := make([]lockfile.Coordinate, 0, len(pin.Dependencies))
	for _, dep := range pin.Dependencies {
		if dep.Group == "" || dep.Artifact == "" || dep.Version == "" {
			continue
		}
		deps = append(deps, dep)
	}
	sort.Slice(deps, func(i, j int) bool {
		switch {
		case deps[i].Group != deps[j].Group:
			return deps[i].Group < deps[j].Group
		case deps[i].Artifact != deps[j].Artifact:
			return deps[i].Artifact < deps[j].Artifact
		case deps[i].Version != deps[j].Version:
			return deps[i].Version < deps[j].Version
		default:
			return deps[i].Classifier < deps[j].Classifier
		}
	})
	project := mavenPomProject{
		Xmlns:      "http://maven.apache.org/POM/4.0.0",
		Xsi:        "http://www.w3.org/2001/XMLSchema-instance",
		Schema:     "http://maven.apache.org/POM/4.0.0 http://maven.apache.org/xsd/maven-4.0.0.xsd",
		Model:      "4.0.0",
		GroupID:    pin.Coordinate.Group,
		ArtifactID: pin.Coordinate.Artifact,
		Version:    pin.Coordinate.Version,
		Packaging:  "jar",
	}
	if len(deps) > 0 {
		project.Dependencies = make([]mavenPomDependency, 0, len(deps))
		for _, dep := range deps {
			project.Dependencies = append(project.Dependencies, mavenPomDependency{
				GroupID:    dep.Group,
				ArtifactID: dep.Artifact,
				Version:    dep.Version,
			})
		}
	}
	payload, err := xml.MarshalIndent(project, "", "  ")
	if err != nil {
		return nil, false, err
	}
	return append(append([]byte(xml.Header), payload...), '\n'), true, nil
}

type gradleModuleMetadata struct {
	FormatVersion string                `json:"formatVersion"`
	Component     gradleModuleComponent `json:"component"`
	Variants      []gradleModuleVariant `json:"variants"`
}

type gradleModuleComponent struct {
	Group   string `json:"group"`
	Module  string `json:"module"`
	Version string `json:"version"`
}

type gradleModuleVariant struct {
	Name         string                   `json:"name"`
	Attributes   map[string]string        `json:"attributes,omitempty"`
	Capabilities []gradleModuleCapability `json:"capabilities,omitempty"`
	Dependencies []gradleModuleDependency `json:"dependencies,omitempty"`
	Files        []gradleModuleFile       `json:"files,omitempty"`
}

type gradleModuleCapability struct {
	Group   string `json:"group"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

type gradleModuleDependency struct {
	Group   string `json:"group"`
	Module  string `json:"module"`
	Version struct {
		Requires string `json:"requires"`
	} `json:"version"`
}

type gradleModuleFile struct {
	Name string `json:"name,omitempty"`
	URL  string `json:"url"`
}

func generatedModulePayload(pin lockfile.Pin) ([]byte, bool, error) {
	primaryFiles := gradleModuleFilesForKinds(pin.Files, lockfile.FileKindPrimary)
	if len(primaryFiles) == 0 {
		return nil, false, nil
	}
	mainAttrs := defaultRuntimeAttributes(pin.Attributes, pin.Files)
	variants := []gradleModuleVariant{{
		Name:         runtimeVariantName(mainAttrs),
		Attributes:   mainAttrs,
		Capabilities: gradleModuleCapabilities(pin),
		Dependencies: gradleModuleDependencies(pin.Dependencies),
		Files:        primaryFiles,
	}}
	if sources := gradleModuleFilesForKinds(pin.Files, lockfile.FileKindSources); len(sources) != 0 {
		variants = append(variants, gradleModuleVariant{
			Name:       "sourcesElements",
			Attributes: map[string]string{"org.gradle.category": "documentation", "org.gradle.docstype": "sources"},
			Files:      sources,
		})
	}
	if javadoc := gradleModuleFilesForKinds(pin.Files, lockfile.FileKindJavadoc); len(javadoc) != 0 {
		variants = append(variants, gradleModuleVariant{
			Name:       "javadocElements",
			Attributes: map[string]string{"org.gradle.category": "documentation", "org.gradle.docstype": "javadoc"},
			Files:      javadoc,
		})
	}
	payload, err := json.MarshalIndent(gradleModuleMetadata{
		FormatVersion: "1.1",
		Component: gradleModuleComponent{
			Group:   pin.Coordinate.Group,
			Module:  pin.Coordinate.Artifact,
			Version: pin.Coordinate.Version,
		},
		Variants: variants,
	}, "", "  ")
	if err != nil {
		return nil, false, err
	}
	return append(payload, '\n'), true, nil
}

func gradleModuleFilesForKinds(files []lockfile.PinFile, kinds ...lockfile.FileKind) []gradleModuleFile {
	if len(files) == 0 {
		return nil
	}
	kindSet := map[lockfile.FileKind]bool{}
	for _, kind := range kinds {
		kindSet[kind] = true
	}
	out := make([]gradleModuleFile, 0, len(files))
	for _, file := range files {
		if !kindSet[file.Kind] {
			continue
		}
		out = append(out, gradleModuleFile{Name: file.Name, URL: file.Name})
	}
	if len(out) == 0 {
		return nil
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].URL != out[j].URL {
			return out[i].URL < out[j].URL
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func defaultRuntimeAttributes(attrs map[string]string, files []lockfile.PinFile) map[string]string {
	out := map[string]string{}
	for key, value := range attrs {
		out[key] = value
	}
	if out["org.gradle.usage"] == "" {
		out["org.gradle.usage"] = "java-runtime"
	}
	if out["org.jetbrains.kotlin.platform.type"] == "" {
		switch primaryArtifactExtension(files) {
		case ".aar":
			out["org.jetbrains.kotlin.platform.type"] = "androidJvm"
		case ".jar":
			out["org.jetbrains.kotlin.platform.type"] = "jvm"
		}
	}
	return out
}

func primaryArtifactExtension(files []lockfile.PinFile) string {
	for _, file := range files {
		if file.Kind == lockfile.FileKindPrimary {
			return strings.ToLower(path.Ext(file.Name))
		}
	}
	return ""
}

func runtimeVariantName(attrs map[string]string) string {
	if attrs["org.gradle.usage"] == "java-api" {
		return "apiElements"
	}
	return "runtimeElements"
}

func gradleModuleCapabilities(pin lockfile.Pin) []gradleModuleCapability {
	if len(pin.Capabilities) == 0 {
		return nil
	}
	out := make([]gradleModuleCapability, 0, len(pin.Capabilities))
	for _, capability := range pin.Capabilities {
		parts := strings.Split(capability, ":")
		if len(parts) != 3 {
			continue
		}
		out = append(out, gradleModuleCapability{
			Group:   strings.TrimSpace(parts[0]),
			Name:    strings.TrimSpace(parts[1]),
			Version: strings.TrimSpace(parts[2]),
		})
	}
	if len(out) == 0 {
		return nil
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Group != out[j].Group {
			return out[i].Group < out[j].Group
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Version < out[j].Version
	})
	return out
}

func gradleModuleDependencies(deps []lockfile.Coordinate) []gradleModuleDependency {
	if len(deps) == 0 {
		return nil
	}
	out := make([]gradleModuleDependency, 0, len(deps))
	for _, dep := range deps {
		if dep.Group == "" || dep.Artifact == "" || dep.Version == "" {
			continue
		}
		var item gradleModuleDependency
		item.Group = dep.Group
		item.Module = dep.Artifact
		item.Version.Requires = dep.Version
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Group != out[j].Group {
			return out[i].Group < out[j].Group
		}
		if out[i].Module != out[j].Module {
			return out[i].Module < out[j].Module
		}
		return out[i].Version.Requires < out[j].Version.Requires
	})
	return out
}

var _ publish.Publisher = (*Publisher)(nil)
