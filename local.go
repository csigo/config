package config

/*local.go contains two local implementation of config:
- FileConfig maps local files to config.
- MemConfig maps memory to config.
*/

import (
	"bufio"
	"bytes"
	"container/list"
	"fmt"
	"io/ioutil"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	log "github.com/Sirupsen/logrus"
)

var (
	// Make file io functions as variable for testing.
	readFile      = ioutil.ReadFile
	readDir       = ioutil.ReadDir
	statusPattern = regexp.MustCompile(`^[MARUC? ][MARUC? ]\s.+$`)
)

// NewFileConfig return filepath instance for local repo. Optionally you can set
// uncommitOnly to true which causes List() method only return a subset of uncommitted
// files.
func NewFileConfig(root string, uncommitOnly bool) (Client, error) {
	if root == "" {
		return nil, fmt.Errorf("need to specify a root path")
	}
	whiteList := map[string]struct{}(nil)
	var err error
	if uncommitOnly {
		whiteList, err = configDiffs(root)
		if err != nil {
			log.Errorf("unable to construct whiteList on root %v, err %v", root, err)
			return nil, err
		}
	}
	return &FileConfig{
		whiteList: whiteList,
		root:      root,
	}, nil
}

// FileConfig is used to test config file in local
type FileConfig struct {
	dummyFuncs
	root      string
	whiteList map[string]struct{}
}

// Get get the file from local directory under root
func (l *FileConfig) Get(path string) ([]byte, error) {
	// path = strings.TrimPrefix(path, l.root)
	return readFile(filepath.Join(l.root, path))
}

// List list all the files of the path
func (l *FileConfig) List(path string) (map[string][]byte, error) {
	f := make(map[string][]byte)
	// path = strings.TrimPrefix(path, l.root)
	// dirs keeps track of all directories to be traversed
	dirs := list.New()
	dirs.PushBack(path)
	for e := dirs.Front(); e != nil; e = e.Next() {
		curPath := e.Value.(string)
		files, err := readDir(filepath.Join(l.root, curPath))
		if err != nil {
			log.Errorf("unable to load root %v, path %v, err %v", l.root, e.Value, err)
			return nil, err
		}
		for _, file := range files {
			// file.Name() only shows the last element of whole path. Need to combine a full
			// path
			fp := filepath.Join(curPath, file.Name())
			if !file.IsDir() {
				// if whitelist isn't nil, see if the file is in whitelist.
				if l.whiteList != nil {
					if _, ok := l.whiteList[fp]; !ok {
						log.Infof("path %s is ignored because of unchanged", fp)
						continue
					}
				}
				f[fp], _ = l.Get(fp)
			} else {
				dirs.PushBack(fp)
			}
		}
	}
	return f, nil
}

// Stop is to stop config client
func (l *FileConfig) Stop() error {
	return fmt.Errorf("not implemented")
}

// NewMemConfig creates a memory mapped config
func NewMemConfig(files map[string][]byte) Client {
	if files == nil {
		files = make(map[string][]byte)
	}
	return &MemConfig{files: files}
}

// MemConfig is a memroy mapped config client
type MemConfig struct {
	dummyFuncs
	files map[string][]byte
}

// Get gets file content
func (m *MemConfig) Get(filename string) ([]byte, error) {
	b, ok := m.files[filename]
	if !ok {
		return nil, fmt.Errorf("file %v doesn't exist", filename)
	}
	return b, nil
}

// List lists files under the path
// TODO: it only returns file without any directories.
func (m *MemConfig) List(path string) (map[string][]byte, error) {
	f := make(map[string][]byte)
	for name, content := range m.files {
		if filepath.HasPrefix(name, path) {
			f[name] = content
		}
	}
	return f, nil
}

// Stop is to stop config client
func (m *MemConfig) Stop() error {
	return fmt.Errorf("not implemented")
}

type dummyFuncs struct{}

func (d dummyFuncs) ConfigInfo() ConfigInfo {
	log.Warning("dummy ConfigInfo() is called. It does nothing")
	return ConfigInfo{}
}

func (d dummyFuncs) AddListener(pathRegEx *regexp.Regexp) *chan ModifiedFile {
	log.Warning("dummy AddListener() is called. It does nothing")
	ch := make(chan ModifiedFile)
	return &ch
}

func (d dummyFuncs) Watch(string, func([]byte) error, chan<- error) error {
	log.Warning("dummy Watch() is called. It does nothing")
	return nil
}

// RemoveListener remove a listener channel
func (d dummyFuncs) RemoveListener(ch *chan ModifiedFile) {
	log.Warning("dummy RemoveListener() is called. It does nothing")
	return
}

// configDiffs leverages git to get changed config lists.
func configDiffs(base string) (map[string]struct{}, error) {
	base, err := filepath.Abs(base)
	if !strings.HasSuffix(base, string(filepath.Separator)) {
		base += string(filepath.Separator)
	}
	if err != nil {
		return nil, err
	}
	out, err := runCmd(base, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	gitroot := strings.Trim(string(out), "\n")
	log.Infof("config git root %s", gitroot)

	results := map[string]struct{}{}
	out, err = runCmd(base, "git", "status", "--porcelain", "-uall")
	if err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if !statusPattern.MatchString(line) {
			continue
		}
		path := filepath.Join(gitroot, line[3:])
		if !strings.HasPrefix(path, base) {
			continue
		}
		results[path[len(base):]] = struct{}{}
	}
	return results, nil
}

// runCmd executes a command with given working directory
var runCmd = func(workingDir, executable string, args ...string) ([]byte, error) {
	cmd := exec.Command(executable, args...)
	// set working dir to cfg
	cmd.Dir = workingDir
	return cmd.CombinedOutput()
}
