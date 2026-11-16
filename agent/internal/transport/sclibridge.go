package transport

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const MaxReadBatch = 100

var knownPaths = map[string][]string{
	"linux": {
		"/usr/local/bin/sclibridge",
	},
	"darwin": {
		"/Users/Shared/Savant/Applications/RacePointMedia/sclibridge",
		"/Users/RPM/Applications/RacePointMedia/sclibridge",
		"~/Applications/RacePointMedia/sclibridge",
		"/Applications/RacePointMedia/sclibridge",
		"/usr/local/bin/sclibridge",
	},
}

var searchRoots = map[string][]string{
	"linux":  {"/usr/local", "/opt", "/data"},
	"darwin": {"/Users/Shared/Savant", "/Users/*/Applications", "/Applications", "/usr/local"},
}

type SclibridgeOptions struct {
	Path        string
	Timeout     time.Duration
	BatchSize   int
	Concurrency int
	Env         map[string]string
	Logger      *slog.Logger
}

type Sclibridge struct {
	path string
	opts SclibridgeOptions
	env  []string
	sem  chan struct{}
	log  *slog.Logger

	mu        sync.Mutex
	multiline map[string]bool
}

func NewSclibridge(opts SclibridgeOptions) (*Sclibridge, error) {
	if opts.Timeout <= 0 {
		opts.Timeout = 10 * time.Second
	}
	if opts.BatchSize <= 0 || opts.BatchSize > MaxReadBatch {
		opts.BatchSize = MaxReadBatch
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 2
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	path := opts.Path
	if path == "" {
		var err error
		if path, err = LocateSclibridge(); err != nil {
			return nil, err
		}
	} else if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("sclibridge path %q: %w", path, err)
	}
	s := &Sclibridge{
		path:      path,
		opts:      opts,
		env:       buildEnv(path, opts.Env),
		sem:       make(chan struct{}, opts.Concurrency),
		log:       opts.Logger,
		multiline: map[string]bool{},
	}
	return s, nil
}

func (s *Sclibridge) Path() string { return s.path }

func LocateSclibridge() (string, error) {
	for _, p := range knownPaths[runtime.GOOS] {
		p = expandHome(p)
		if isExecutable(p) {
			return p, nil
		}
	}
	if p, err := exec.LookPath("sclibridge"); err == nil {
		return p, nil
	}
	for _, root := range searchRoots[runtime.GOOS] {
		matches, _ := filepath.Glob(expandHome(root))
		for _, m := range matches {
			if p := findFile(m, "sclibridge", 6); p != "" {
				return p, nil
			}
		}
	}
	return "", errors.New("sclibridge not found; set sclibridge in the agent config")
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, p[2:])
		}
	}
	return p
}

func isExecutable(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular() && fi.Mode()&0o111 != 0
}

func findFile(root, name string, maxDepth int) string {
	var found string
	base := strings.Count(filepath.Clean(root), string(os.PathSeparator))
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if strings.Count(p, string(os.PathSeparator))-base >= maxDepth {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == name && isExecutable(p) {
			found = p
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// sclibridge on the SHR-2000 links against /usr/local/lib and won't start without
// it on the library path. Savant's own daemons run the same way.
func buildEnv(binPath string, extra map[string]string) []string {
	env := os.Environ()
	if runtime.GOOS == "linux" {
		libDirs := []string{}
		for _, d := range []string{"/usr/local/lib", filepath.Join(filepath.Dir(binPath), "..", "lib")} {
			if fi, err := os.Stat(d); err == nil && fi.IsDir() {
				libDirs = append(libDirs, filepath.Clean(d))
			}
		}
		if len(libDirs) > 0 {
			cur := os.Getenv("LD_LIBRARY_PATH")
			v := strings.Join(libDirs, ":")
			if cur != "" {
				v += ":" + cur
			}
			env = setEnv(env, "LD_LIBRARY_PATH", v)
		}
	}
	keys := make([]string, 0, len(extra))
	for k := range extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		env = setEnv(env, k, extra[k])
	}
	return env
}

func setEnv(env []string, k, v string) []string {
	prefix := k + "="
	for i, e := range env {
		if strings.HasPrefix(e, prefix) {
			env[i] = prefix + v
			return env
		}
	}
	return append(env, prefix+v)
}

func (s *Sclibridge) run(ctx context.Context, args ...string) (string, error) {
	return s.runTimeout(ctx, s.opts.Timeout, args...)
}

func (s *Sclibridge) runTimeout(ctx context.Context, timeout time.Duration, args ...string) (string, error) {
	select {
	case s.sem <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	defer func() { <-s.sem }()

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.path, args...)
	cmd.Env = s.env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	start := time.Now()
	err := cmd.Run()
	s.log.Debug("sclibridge", "cmd", args[0], "args", len(args)-1, "dur", time.Since(start), "err", err)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("sclibridge %s: timed out after %s", args[0], timeout)
		}
		return "", fmt.Errorf("sclibridge %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

func splitLines(out string) []string {
	out = strings.ReplaceAll(out, "\r\n", "\n")
	if out == "" {
		return nil
	}
	out = strings.TrimSuffix(out, "\n")
	return strings.Split(out, "\n")
}

func nonEmptyLines(out string) []string {
	var res []string
	for _, l := range splitLines(out) {
		if l = strings.TrimSpace(l); l != "" {
			res = append(res, l)
		}
	}
	return res
}

func (s *Sclibridge) ListZones(ctx context.Context) ([]string, error) {
	out, err := s.run(ctx, "userzones")
	if err != nil {
		return nil, err
	}
	return nonEmptyLines(out), nil
}

func (s *Sclibridge) ListServices(ctx context.Context, zone string) ([]Service, error) {
	out, err := s.run(ctx, "servicesforzone", zone)
	if err != nil {
		return nil, err
	}
	var res []Service
	for _, line := range nonEmptyLines(out) {
		svc, err := ParseServiceLine(zone, line, nil)
		if err != nil {
			s.log.Warn("skipping unparseable service line", "zone", zone, "line", line, "err", err)
			continue
		}
		res = append(res, svc)
	}
	return res, nil
}

// The filter argument returns nothing on Savant 11, so we filter here instead.
func (s *Sclibridge) StateNames(ctx context.Context, filter string) ([]string, error) {
	var re *regexp.Regexp
	if filter != "" {
		var err error
		if re, err = regexp.Compile(filter); err != nil {
			return nil, err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	out, err := s.runTimeout(ctx, 2*time.Minute, "statenames")
	if err != nil {
		return nil, err
	}
	var res []string
	for _, n := range nonEmptyLines(out) {
		if re == nil || re.MatchString(n) {
			res = append(res, n)
		}
	}
	return res, nil
}

func (s *Sclibridge) ReadStates(ctx context.Context, names []string) (map[string]string, error) {
	res := make(map[string]string, len(names))
	var batch []string
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		err := s.readBatch(ctx, batch, res)
		batch = batch[:0]
		return err
	}
	for _, n := range dedupe(names) {
		s.mu.Lock()
		solo := s.multiline[n]
		s.mu.Unlock()
		if solo {
			if err := s.readBatch(ctx, []string{n}, res); err != nil {
				return res, err
			}
			continue
		}
		batch = append(batch, n)
		if len(batch) == s.opts.BatchSize {
			if err := flush(); err != nil {
				return res, err
			}
		}
	}
	return res, flush()
}

func (s *Sclibridge) readBatch(ctx context.Context, names []string, res map[string]string) error {
	args := append([]string{"readstate"}, names...)
	out, err := s.run(ctx, args...)
	if err != nil {
		return err
	}
	lines := splitLines(out)
	if len(names) == 1 {
		v := strings.Join(lines, "\n")
		if len(lines) > 1 {
			s.mu.Lock()
			if !s.multiline[names[0]] {
				s.log.Info("state has a multi-line value; reading it separately", "state", names[0])
			}
			s.multiline[names[0]] = true
			s.mu.Unlock()
		}
		res[names[0]] = v
		return nil
	}
	if len(lines) == len(names) {
		for i, n := range names {
			res[n] = lines[i]
		}
		return nil
	}
	// A value containing a newline throws off the line count. Split the batch
	// until that state is found, then read it on its own from now on.
	s.log.Debug("readstate line count mismatch; bisecting", "want", len(names), "got", len(lines))
	mid := len(names) / 2
	left := append([]string(nil), names[:mid]...)
	right := append([]string(nil), names[mid:]...)
	if err := s.readBatch(ctx, left, res); err != nil {
		return err
	}
	return s.readBatch(ctx, right, res)
}

func dedupe(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := in[:0:0]
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func (s *Sclibridge) ServiceRequest(ctx context.Context, req Request) error {
	if req.Name == "" {
		return errors.New("service request: empty request name")
	}
	sv := req.Service
	args := []string{"servicerequest", sv.Zone, sv.Component, sv.LogicalComponent, sv.Variant, sv.Type, req.Name}
	for _, k := range orderedKeys(req.Args, req.ArgOrder) {
		args = append(args, k, req.Args[k])
	}
	_, err := s.run(ctx, args...)
	return err
}

func orderedKeys(m map[string]string, order []string) []string {
	keys := make([]string, 0, len(m))
	used := map[string]bool{}
	for _, k := range order {
		if _, ok := m[k]; ok && !used[k] {
			keys = append(keys, k)
			used[k] = true
		}
	}
	var rest []string
	for k := range m {
		if !used[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	return append(keys, rest...)
}

func (s *Sclibridge) Scenes(ctx context.Context) ([]Scene, error) {
	f, err := os.CreateTemp("", "savant-ha-scenes-*.csv")
	if err != nil {
		return nil, err
	}
	path := f.Name()
	f.Close()
	defer os.Remove(path)
	if _, err := s.run(ctx, "getSceneNames", path); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseScenesCSV(data)
}

func ParseScenesCSV(data []byte) ([]Scene, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	r.TrimLeadingSpace = true
	var res []Scene
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return res, fmt.Errorf("scene csv: %w", err)
		}
		if len(rec) < 2 || strings.TrimSpace(rec[0]) == "" {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(rec[0]), "name") && strings.EqualFold(strings.TrimSpace(rec[1]), "id") {
			continue
		}
		sc := Scene{Name: strings.TrimSpace(rec[0]), ID: strings.TrimSpace(rec[1])}
		if len(rec) > 2 {
			sc.User = strings.TrimSpace(rec[2])
		}
		res = append(res, sc)
	}
	return res, nil
}

func (s *Sclibridge) ActivateScene(ctx context.Context, sc Scene) error {
	_, err := s.run(ctx, "activateScene", sc.Name, sc.ID, sc.User)
	return err
}
