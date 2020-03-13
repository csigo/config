package config

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
)

var (
	testFiles = map[string][]byte{
		"a/b/c": []byte("abc"),
		"a/b/d": []byte("abd"),
		"a/c":   []byte("ac"),
	}
)

//------------------------------------------------------------------------------
// FileConfig tests
//------------------------------------------------------------------------------
type FileConfigSuite struct {
	suite.Suite
	cfg Client
}

func TestFileConfigSuite(t *testing.T) {
	suite.Run(t, new(FileConfigSuite))
}

func (s *FileConfigSuite) SetupSuite() {
	readFile = readMemFile
	readDir = readMemDir
}

func (s *FileConfigSuite) TearDownSuite() {
	readFile = ioutil.ReadFile
	readDir = ioutil.ReadDir
}

func (s *FileConfigSuite) SetupTest() {
	var err error
	s.cfg, err = NewFileConfig("a/", false)
	assert.NoError(s.T(), err)
}

func (s *FileConfigSuite) TestGet() {
	b, err := s.cfg.Get("b/c")
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), []byte("abc"), b)
}

func (s *FileConfigSuite) TestGetFailed() {
	_, err := s.cfg.Get("b")
	assert.Error(s.T(), err)
}

func (s *FileConfigSuite) TestList() {
	files, err := s.cfg.List("b")
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), 2, len(files))
	for k, v := range files {
		assert.Equal(s.T(), testFiles["a/"+k], v)
	}
}

func (s *FileConfigSuite) TestListFailed() {
	_, err := s.cfg.List("c")
	assert.Error(s.T(), err)
}

func readMemFile(filename string) ([]byte, error) {
	v, ok := testFiles[filename]
	if !ok {
		err := fmt.Errorf("file %v doesn't exit", filename)
		logrus.Error(err)
		return nil, err
	}
	return v, nil
}

func readMemDir(dirname string) ([]os.FileInfo, error) {
	files := []os.FileInfo{}
	for name := range testFiles {
		dir := filepath.Dir(name)
		if strings.HasPrefix(dir, dirname) {
			files = append(files, &fileStat{name: name, dir: !(dir == dirname)})
		}
	}
	if len(files) == 0 {
		err := fmt.Errorf("dir %v doesn't exit", dirname)
		logrus.Error(err)
		return nil, err
	}
	return files, nil
}

type fileStat struct {
	name string
	dir  bool
}

func (fs *fileStat) Name() string       { return fs.name }
func (fs *fileStat) Size() int64        { return 0 }
func (fs *fileStat) Mode() os.FileMode  { return 0 }
func (fs *fileStat) ModTime() time.Time { return time.Unix(0, 0) }
func (fs *fileStat) IsDir() bool        { return fs.dir }
func (fs *fileStat) Sys() interface{}   { return nil }

//------------------------------------------------------------------------------
// MemConfig tests
//------------------------------------------------------------------------------
type MemConfigSuite struct {
	suite.Suite
	cfg Client
}

func TestMemConfigSuite(t *testing.T) {
	suite.Run(t, new(MemConfigSuite))
}

func (s *MemConfigSuite) SetupTest() {
	s.cfg = NewMemConfig(testFiles)
}

func (s *MemConfigSuite) TestGet() {
	name := "a/c"
	b, err := s.cfg.Get(name)
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), testFiles[name], b)
}

func (s *MemConfigSuite) TestGetFailed() {
	name := "a/d"
	_, err := s.cfg.Get(name)
	assert.Error(s.T(), err)
}

func (s *MemConfigSuite) TestList() {
	b, err := s.cfg.List("a/b")
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), 2, len(b))
	for k, v := range b {
		assert.Equal(s.T(), testFiles[k], v)
	}
}

type runCmdMock struct {
	mock.Mock
}

func (m *runCmdMock) Run(workingDir, executable string, arg ...string) ([]byte, error) {
	a := []interface{}{workingDir, executable}
	for _, v := range arg {
		a = append(a, v)
	}
	args := m.Called(a...)
	return args.Get(0).([]byte), args.Error(1)
}

func TestUtilSuite(t *testing.T) {
	suite.Run(t, new(UtilSuite))
}

type UtilSuite struct {
	suite.Suite
}

func (s *UtilSuite) TestDummy() {

	m := &runCmdMock{}
	m.On("Run", "/aaa/bbb/", "git", "rev-parse", "--show-toplevel").Return([]byte("/aaa\n"), nil)
	m.On("Run", "/aaa/bbb/", "git", "status", "--porcelain", "-uall").Return([]byte(`
?? bbb/ccc/ddd.yaml
?? bbb/ccc/eee
?? bbb/ccc/fff.bak
 M bbb/ccc/ggg.yaml
 M xxx/yyy/zzz.yaml
 M xxx/yyy/uuu.bak
123412341234
`), nil)

	runCmd = m.Run
	result, err := configDiffs("/aaa/bbb")
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), len(result), 4)
}
