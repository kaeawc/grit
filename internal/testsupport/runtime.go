package testsupport

import "sync"

type CommandCall struct {
	Name string
	Args []string
}

type CommandRunnerRecorder struct {
	mu    sync.Mutex
	Calls []CommandCall
	Err   error
}

func (r *CommandRunnerRecorder) Run(name string, args ...string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Calls = append(r.Calls, CommandCall{
		Name: name,
		Args: append([]string(nil), args...),
	})
	return r.Err
}

type DeviceRecorder struct {
	mu         sync.Mutex
	Installs   []string
	Uninstalls []string
	Err        error
}

func (d *DeviceRecorder) Install(apkPath string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.Installs = append(d.Installs, apkPath)
	return d.Err
}

func (d *DeviceRecorder) Uninstall(packageName string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.Uninstalls = append(d.Uninstalls, packageName)
	return d.Err
}

type ArtifactStoreRecorder struct {
	mu      sync.Mutex
	Loads   []string
	Saves   []string
	Entries map[string][]byte
}

func NewArtifactStoreRecorder() *ArtifactStoreRecorder {
	return &ArtifactStoreRecorder{Entries: map[string][]byte{}}
}

func (s *ArtifactStoreRecorder) Load(key string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Loads = append(s.Loads, key)
	data, ok := s.Entries[key]
	if !ok {
		return nil, false
	}
	return append([]byte(nil), data...), true
}

func (s *ArtifactStoreRecorder) Save(key string, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Saves = append(s.Saves, key)
	s.Entries[key] = append([]byte(nil), data...)
}

type DependencyResolverRecorder struct {
	mu      sync.Mutex
	Queries []string
	Results map[string][]string
}

func NewDependencyResolverRecorder() *DependencyResolverRecorder {
	return &DependencyResolverRecorder{Results: map[string][]string{}}
}

func (r *DependencyResolverRecorder) Resolve(module string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Queries = append(r.Queries, module)
	return append([]string(nil), r.Results[module]...)
}
