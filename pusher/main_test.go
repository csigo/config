package main

// This gofile contains testcases for main.go

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/coreos/go-etcd/etcd"
	"github.com/csigo/config"
	"github.com/csigo/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type ConfigTestSuite struct {
	suite.Suite
	prompts int

	// sl is the zookeeper service.
	sl test.ServiceLauncher
	// slStopFunc is the stop function for ServiceLauncher.
	slStopFunc func() error
	// etcdConn is the etcd client.
	etcdConn *etcd.Client
	// etcdRoot is the root of etcdnode.
	etcdRoot string

	// folder simulates a directory of file system
	// we keep the data in here for the sake of comparison with the data in etcd.
	// key is a file path and the value is its content.
	folder map[string]*GitFile
	// git provides git functionality.
	git *GitUtil
}

const (
	etcdDelimiter = ","
)

// SetupSuite setups the testing environment.
func (s *ConfigTestSuite) SetupSuite() {
	var err error

	s.sl = test.NewServiceLauncher()
	port, stop, err := s.sl.Start(test.Etcd)
	s.slStopFunc = stop
	if err != nil {
		log.Fatalf("etcd failed, %s", err)
	}
	assert.NoError(s.T(), err)
	etcdEnsemble := fmt.Sprintf("http://localhost:%d", port)
	s.etcdConn = etcd.NewClient(strings.Split(etcdEnsemble, etcdDelimiter))
	assert.NotNil(s.T(), s.etcdConn)

	promptUser = func(format string, args ...interface{}) {
		fmt.Printf(format+"\n", args...)
		s.prompts++
	}
}

// TearDownSuite cleans up the testing environment.
func (s *ConfigTestSuite) TearDownSuite() {
	s.etcdConn.Close()
	s.slStopFunc()
}

// SetupTest setups for each testcase.
func (s *ConfigTestSuite) SetupTest() {
	// init etcd znode
	s.etcdRoot = "/testconfig"
	s.etcdConn.Delete(s.etcdRoot, true)
	_, err := s.etcdConn.SetDir(s.etcdRoot, 0)
	assert.NoError(s.T(), err)

	// Setup git util
	s.git, err = NewGitUtil()
	assert.NoError(s.T(), err)
	s.folder = make(map[string]*GitFile)
	s.prompts = 0
}

// TearDownTest cleans up the environment for each testcases.
func (s *ConfigTestSuite) TearDownTest() {
	s.etcdConn.Delete(s.etcdRoot, true)
}

// TestBasicPusher tests the basic pusher function from local repo to etcd.
func (s *ConfigTestSuite) TestBasicCommit() {
	s.folder["file1"] = NewGitFile("e", "this is file1")
	s.folder["file2"] = NewGitFile("e", "this is file2")
	ExecStrCmds(s.git.CommitCmds(s.folder))

	// do the pusher first and then verify the data
	Pusher(s.etcdConn, s.git.WorkDir(), s.etcdRoot)
	s.verifyFolders()
	assert.Equal(s.T(), 2, s.prompts)
}

// TestBasicPusher tests the basic pusher function from local repo to etcd.
func (s *ConfigTestSuite) TestMoreCommit() {
	s.folder["file1"] = NewGitFile("e", "this is file1")
	s.folder["file2"] = NewGitFile("e", "this is file2")
	ExecStrCmds(s.git.CommitCmds(s.folder))

	s.folder["file3"] = NewGitFile("e", "this is file3")
	s.folder["file2"] = NewGitFile("e", "this is file2 with modified")
	ExecStrCmds(s.git.CommitCmds(s.folder))

	s.folder["file4"] = NewGitFile("e", "this is file4")
	s.folder["file2"] = NewGitFile("e", "this is file2 with second modified")
	ExecStrCmds(s.git.CommitCmds(s.folder))

	Pusher(s.etcdConn, s.git.WorkDir(), s.etcdRoot)
	s.verifyFolders()
	assert.Equal(s.T(), 2, s.prompts)
}

// TestBranchMerge tests the feature of branching and merging.
func (s *ConfigTestSuite) TestBranchMerge() {
	s.folder["file1"] = NewGitFile("e", "this is file1")
	s.folder["file2"] = NewGitFile("e", "this is file2")
	ExecStrCmds(s.git.CommitCmds(s.folder))

	s.folder["file1"] = NewGitFile("e", "this is file1")
	s.folder["file2"] = NewGitFile("e", "this is file2")
	ExecStrCmds(s.git.CommitCmds(s.folder))

	branch := "feature1"
	ExecStrCmds(s.git.BranchCmds(branch))
	s.folder["file4"] = NewGitFile("e", "this is file4 - in branch1")
	s.folder["file1"] = NewGitFile("e", "this is file2 - in branch1")
	ExecStrCmds(s.git.CommitCmds(s.folder))
	ExecStrCmds(s.git.Merge2MasterCmds(branch))

	Pusher(s.etcdConn, s.git.WorkDir(), s.etcdRoot)
	s.verifyFolders()
	assert.Equal(s.T(), 2, s.prompts)
}

// TestDeleteFiles tests the feature of deleting.
func (s *ConfigTestSuite) TestDeleteFiles() {
	s.folder["file1"] = NewGitFile("e", "this is file1")
	s.folder["file2"] = NewGitFile("e", "this is file2")
	ExecStrCmds(s.git.CommitCmds(s.folder))

	Pusher(s.etcdConn, s.git.WorkDir(), s.etcdRoot)
	s.verifyFolders()
	assert.Equal(s.T(), 2, s.prompts)

	s.folder["file1"] = NewGitFile("d", "")
	s.folder["file2"] = NewGitFile("e", "this is file2")
	ExecStrCmds(s.git.CommitCmds(s.folder))

	Pusher(s.etcdConn, s.git.WorkDir(), s.etcdRoot)
	s.verifyFolders()
	assert.Equal(s.T(), 3, s.prompts)
}

// TestDeleteFiles tests file deleting when git TOT is not passed as file root
func (s *ConfigTestSuite) TestDeleteFilesSubDir() {
	// create dir
	_, err := s.etcdConn.SetDir(filepath.Join(s.etcdRoot, "dir"), 0)
	assert.NoError(s.T(), err)

	s.folder = nil
	s.folder = make(map[string]*GitFile)

	s.folder["dir/file1"] = NewGitFile("e", "this is file1")
	s.folder["dir/file2"] = NewGitFile("e", "this is file2")
	ExecStrCmds(s.git.CommitCmds(s.folder))

	s.folder["dir/file1"] = NewGitFile("d", "")
	s.folder["dir/file2"] = NewGitFile("e", "this is file2")
	ExecStrCmds(s.git.CommitCmds(s.folder))

	Pusher(s.etcdConn,
		filepath.Join(s.git.WorkDir(), "dir"),
		filepath.Join(s.etcdRoot, "dir"),
	)
	s.verifyFolders()
	assert.Equal(s.T(), 2, s.prompts)
}

// TestMultiPusher tests the feature of multiple pusher.
func (s *ConfigTestSuite) TestMultiPusher() {
	s.folder["file1"] = NewGitFile("e", "this is file1")
	s.folder["file2"] = NewGitFile("e", "this is file2")
	ExecStrCmds(s.git.CommitCmds(s.folder))
	Pusher(s.etcdConn, s.git.WorkDir(), s.etcdRoot)
	assert.Equal(s.T(), 2, s.prompts)

	branch := "feature1"
	ExecStrCmds(s.git.BranchCmds(branch))
	s.folder["file4"] = NewGitFile("e", "this is file4 - in branch1")
	s.folder["file1"] = NewGitFile("e", "this is file2 - in branch1")
	ExecStrCmds(s.git.CommitCmds(s.folder))
	ExecStrCmds(s.git.Merge2MasterCmds(branch))
	Pusher(s.etcdConn, s.git.WorkDir(), s.etcdRoot)
	assert.Equal(s.T(), 3, s.prompts)

	s.folder["file1"] = NewGitFile("d", "")
	s.folder["file2"] = NewGitFile("e", "this is file2")
	ExecStrCmds(s.git.CommitCmds(s.folder))
	Pusher(s.etcdConn, s.git.WorkDir(), s.etcdRoot)
	s.verifyFolders()
	assert.Equal(s.T(), 4, s.prompts)
}

// TestDirectories tests the case of directory.
func (s *ConfigTestSuite) TestDirectories() {
	s.folder["dir1/file1"] = NewGitFile("e", "this is file1 in dir1")
	s.folder["dir1/file2"] = NewGitFile("e", "this is file2 in dir1")
	ExecStrCmds(s.git.CommitCmds(s.folder))
	Pusher(s.etcdConn, s.git.WorkDir(), s.etcdRoot)
	assert.Equal(s.T(), 2, s.prompts)

	s.folder["dir2/file1"] = NewGitFile("e", "this is file1 in dir2")
	s.folder["dir2/file2"] = NewGitFile("e", "this is file2 in dir2")
	ExecStrCmds(s.git.CommitCmds(s.folder))
	Pusher(s.etcdConn, s.git.WorkDir(), s.etcdRoot)
	assert.Equal(s.T(), 3, s.prompts)

	s.folder["dir2/file1"] = NewGitFile("d", "")
	s.folder["dir2/file2"] = NewGitFile("d", "")
	ExecStrCmds(s.git.CommitCmds(s.folder))
	Pusher(s.etcdConn, s.git.WorkDir(), s.etcdRoot)
	s.verifyFolders()
	assert.Equal(s.T(), 4, s.prompts)
}

// TestDirectoreisX tests the case of directory.
func (s *ConfigTestSuite) TestDirectoriesX() {
	s.folder["dirX1/file1"] = NewGitFile("e", "this is file1 in dirX1")
	s.folder["dirX1/file2"] = NewGitFile("e", "this is file2 in dirX1")
	ExecStrCmds(s.git.CommitCmds(s.folder))
	Pusher(s.etcdConn, s.git.WorkDir(), s.etcdRoot)
	assert.Equal(s.T(), 2, s.prompts)

	s.folder["dirX2/file1"] = NewGitFile("e", "this is file1 in dirX2")
	s.folder["dirX2/file2"] = NewGitFile("e", "this is file2 in dirX2")
	ExecStrCmds(s.git.CommitCmds(s.folder))
	Pusher(s.etcdConn, s.git.WorkDir(), s.etcdRoot)
	assert.Equal(s.T(), 3, s.prompts)

	s.folder["dirX2/file1"] = NewGitFile("d", "")
	ExecStrCmds(s.git.CommitCmds(s.folder))
	Pusher(s.etcdConn, s.git.WorkDir(), s.etcdRoot)
	assert.Equal(s.T(), 4, s.prompts)

	s.verifyFolders()
}

// TestCommitVersion check if commit version is written to etcdroot
func (s *ConfigTestSuite) TestCommitVersion() {
	infoPath := fmt.Sprintf(infoPrefix, s.etcdRoot)
	s.folder["dirCV1/file1"] = NewGitFile("e", "this is file1 in dirCV1")
	s.folder["dirCV1/file2"] = NewGitFile("e", "this is file2 in dirCV1")
	ExecStrCmds(s.git.CommitCmds(s.folder))
	Pusher(s.etcdConn, s.git.WorkDir(), s.etcdRoot)
	assert.Equal(s.T(), 2, s.prompts)
	// check commit
	commitHash := runSingleCmdOrFatal("git rev-parse HEAD")
	resp, err := s.etcdConn.Get(infoPath, true, false)
	assert.NoError(s.T(), err)
	cfg := &config.ConfigInfo{}
	err = json.Unmarshal([]byte(resp.Node.Value), cfg)
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), commitHash, cfg.Version)

	s.folder["dirCV2/file1"] = NewGitFile("e", "this is file1 in dirCV2")
	s.folder["dirCV2/file2"] = NewGitFile("e", "this is file2 in dirCV2")
	ExecStrCmds(s.git.CommitCmds(s.folder))
	Pusher(s.etcdConn, s.git.WorkDir(), s.etcdRoot)
	assert.Equal(s.T(), 3, s.prompts)
	// check 2nd commit
	oldHash := commitHash
	commitHash = runSingleCmdOrFatal("git rev-parse HEAD")
	assert.NotEqual(s.T(), oldHash, commitHash)
	resp, err = s.etcdConn.Get(infoPath, true, false)
	assert.NoError(s.T(), err)
	err = json.Unmarshal([]byte(resp.Node.Value), cfg)
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), commitHash, cfg.Version)
}

// TestSymbolicLink test given root is symlink
func (s *ConfigTestSuite) TestSymbolicLink() {
	// create symbolic link
	testDir, err := ioutil.TempDir("", "")
	symRoot := filepath.Join(testDir, "testgit")
	assert.NoError(s.T(), os.Symlink(s.git.WorkDir(), symRoot), "Symlink should be created")

	// init git
	infoPath := fmt.Sprintf(infoPrefix, s.etcdRoot)
	s.folder["dirCV1/file1"] = NewGitFile("e", "this is file1 in dirCV1")
	s.folder["dirCV1/file2"] = NewGitFile("e", "this is file2 in dirCV1")
	ExecStrCmds(s.git.CommitCmds(s.folder))

	// push with symlink
	Pusher(s.etcdConn, symRoot, s.etcdRoot)
	assert.Equal(s.T(), 2, s.prompts)

	// check result
	commitHash := runSingleCmdOrFatal("git rev-parse HEAD")
	resp, err := s.etcdConn.Get(infoPath, true, false)
	assert.NoError(s.T(), err)
	cfg := &config.ConfigInfo{}
	err = json.Unmarshal([]byte(resp.Node.Value), cfg)
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), commitHash, cfg.Version)
	assert.Equal(s.T(), len(s.folder), len(cfg.ModFiles))
}

// verifyFolders verifies the data contained in folder and that in etcd.
func (s *ConfigTestSuite) verifyFolders() {

	// check the data in etcd
	for expectedFile, expectedVal := range s.folder {
		if expectedVal.St == "e" {
			// check the file which should be in etcd.
			resp, err := s.etcdConn.Get(filepath.Join(s.etcdRoot, expectedFile), true, false)
			assert.NoError(s.T(), err)
			assert.Equal(s.T(), expectedVal.Content, resp.Node.Value, "we should get the exactly same values, but got(%s,%s)", expectedVal, resp.Node.Value)

		} else if expectedVal.St == "d" {
			// the file should not exist in etcd.
			_, err := s.etcdConn.Get(filepath.Join(s.etcdRoot, expectedFile), true, false)
			assert.Error(s.T(), err)
		} else {
			s.T().Fatal("we should not go to here.")
		}
	}
}

// TestConfigTestSuite invokes a normal testcase for Config testsuite.
func TestConfigTestSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}
	suite.Run(t, new(ConfigTestSuite))
}
