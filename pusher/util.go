package main

// util.go contains test related utils.

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/BurntSushi/cmd"
)

// GitUtil wraps git functionality.
type GitUtil struct {
	//workDir is the working dir.
	workDir string
	// gitCmdPrefix stores the prefix of git command.
	gitCmdPrefix string
	// numCommits stores the number of commits.
	numCommits int
}

// NewGitUtil creates a git utility.
func NewGitUtil() (*GitUtil, error) {
	workDir, err := ioutil.TempDir("", "")
	if err != nil {
		return nil, err
	}

	// setup git env
	dir, err := ioutil.TempDir("", "")
	if err != nil {
		return nil, err
	}
	// since some system return the symbolic path from git, we have to convert it back to
	// the real absolute path.
	workDir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	gitDir := filepath.Join(workDir, ".git")
	g := &GitUtil{
		gitCmdPrefix: fmt.Sprintf("git --git-dir %s --work-tree %s", gitDir, workDir),
		workDir:      workDir,
	}
	// init git command
	ExecStrCmds(g.gitInitCmds())
	return g, nil
}

// WorkDir returns git working directory.
func (g *GitUtil) WorkDir() string {
	return g.workDir
}

// gitInitCmds generates the cmds for init commit.
// It actually create a Init file and do an init commit.
func (g *GitUtil) gitInitCmds() []string {
	return []string{
		fmt.Sprintf("%s init", g.gitCmdPrefix),
		// Some repo doesn't have git name/email setup so create one.
		fmt.Sprintf("%s config user.email \"pusher@htc.com\"", g.gitCmdPrefix),
		fmt.Sprintf("%s config user.name \"pusher\"", g.gitCmdPrefix),
		fmt.Sprintf("echo '' > %s/init", g.workDir),
		fmt.Sprintf("%s add --all", g.gitCmdPrefix),
		fmt.Sprintf("%s commit -m 'init commit'", g.gitCmdPrefix),
	}
}

// ExecStrCmds executes the commands.
func ExecStrCmds(strCmds []string) {
	cmds := make([]*exec.Cmd, len(strCmds))
	buffers := make([]bytes.Buffer, len(strCmds))
	for i, v := range strCmds {
		cmds[i] = exec.Command("/bin/bash", "-c", fmt.Sprintf("%s", v))
		cmds[i].Stderr = &buffers[i]
	}

	cmd.NewCmds(cmds).RunMany(1)
	//  FIXME: seems there's some error if we uncomment the followings
	//	errs := cmd.NewCmds(cmds).RunMany(0)
	//	for i, v := range errs {
	//		if v != nil && cmds[i].ProcessState.Success() == false {
	//			s.T().Errorf("Cmd: %v, err: %v, stderr: %s", cmds[i], v, buffers[i].String())
	//			s.T().FailNow()
	//		}
	//	}
}

// CommitCmds generate the cmds from the folder data structure.
// and append a "git add --all" at the end of command.
func (g *GitUtil) CommitCmds(folder map[string]*GitFile) []string {
	cmds := []string{}
	for k, v := range folder {
		if v.St == "d" {
			cmds = append(cmds, fmt.Sprintf("rm %s/%s", g.workDir, k))
		} else {
			dir := filepath.Dir(k)
			os.MkdirAll(filepath.Join(g.workDir, dir), os.ModeDir|os.ModePerm)
			cmds = append(cmds, fmt.Sprintf("echo '%s' > %s/%s", v.Content, g.workDir, k))
		}
	}

	if len(cmds) != 0 {
		cmds = append(cmds, fmt.Sprintf("%s add --all", g.gitCmdPrefix))
		cmds = append(cmds, fmt.Sprintf("%s commit -m '%d:commits'", g.gitCmdPrefix, g.numCommits))
		g.numCommits = g.numCommits + 1
	}
	return cmds
}

// BranchCmds returns the git command for branching.
func (g *GitUtil) BranchCmds(branch string) []string {
	return []string{
		fmt.Sprintf("%s checkout -b %s", g.gitCmdPrefix, branch),
	}
}

// Merge2MasterCmds returns the git command for merging.
func (g *GitUtil) Merge2MasterCmds(branch string) []string {
	return []string{
		fmt.Sprintf("%s checkout master", g.gitCmdPrefix),
		fmt.Sprintf("%s merge %s", g.gitCmdPrefix, branch),
	}
}

// GitFile defines the file status of git stored in memroy.
type GitFile struct {
	//St only contains a binary inforamtion of the file. That is, exist(e) or disappear(d).
	St      string
	Content string
}

// NewGitFile allocates and retruns a gitFile.
func NewGitFile(st, content string) *GitFile {
	return &GitFile{St: st, Content: content}
}
