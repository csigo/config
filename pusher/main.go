package main

// TODO: Add integration test between pusher and client.
import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/cmd"
	"github.com/coreos/go-etcd/etcd"
	"github.com/csigo/config"
	log "github.com/sirupsen/logrus"
)

const (
	gitMinVersion = 1.9
	infoPrefix    = "%s/._info"
)

var (
	rootpath = flag.String("csi.config.repo.root", "", "repo root path")
	etcdroot = flag.String("csi.config.etcd.root", "", "etcd root node for config info")
	machines = flag.String("csi.config.etcd.machines", "", "etcd machines in host:port,host:port,... format")

	// regexp that split a string by whites
	regexSplitWhite *regexp.Regexp
)

func init() {
	regexSplitWhite, _ = regexp.Compile("\\s+")
}

// runSingleCmdOrFatal runs a command line, and fatal if either
// return code is not 0, or stderr is not empty.
// It returns stdout.
func runSingleCmdOrFatal(line string) string {
	// trim the command line
	line = strings.Trim(line, " \t")
	tokens := regexSplitWhite.Split(line, -1)
	if len(tokens) < 1 {
		log.Fatalf("can't execute an empty command line\n")
	}
	command := cmd.New(tokens[0], tokens[1:]...)
	err := command.Run()
	if err != nil {
		log.Fatalf("error running command '%s': %s\n", line, err)
	}
	bs := command.BufStdout.Bytes()
	str := string(bs[:len(bs)])
	// trim \n
	return strings.TrimRight(str, "\n\r")
}

// checkGitVersion makes sure that git version supports this script
func checkGitVersion() {
	re := regexp.MustCompile(`git version ([0-9]+\.[0-9]+)\.`)
	vString := runSingleCmdOrFatal("git --version")
	match := re.FindStringSubmatch(vString)
	v, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		log.Fatalf("Unable to parse git version <%s>, err %s", vString, err)
	}
	fmt.Printf("Current git version %f\n", v)
	if v < gitMinVersion {
		log.Fatalf("Git version: %f. Minimum required version: %f. Aborting.", v, gitMinVersion)
	}
}

// chRepo change current dir to the repo
func cdRepo(path string) {
	err := os.Chdir(path)
	if err != nil {
		log.Fatalf("Unable to change cwd to %s: %s", path, err)
	}
	fmt.Printf("change cwd to: %s\n", path)
}

// filterRelatedFiles takes a commit file log and return a slice of
// ModifiedFile
func filterRelatedFiles(configRoot, repoRoot string, fstr string) *[]config.ModifiedFile {
	ret := []config.ModifiedFile{}

	sz := len(configRoot)
	for _, line := range strings.Split(fstr, "\n") {
		if line == "" {
			continue
		}
		tokens := regexSplitWhite.Split(line, -1)
		if len(tokens) != 2 {
			log.Fatalf("not a valid file change in commit message: >%s<\n", line)
		}
		// repoRoot + file and then excludes anything that is not within configRoot
		fileabs := fmt.Sprintf("%s/%s", repoRoot, tokens[1])
		if len(fileabs) < sz || fileabs[:sz] != configRoot {
			continue
		}
		// Get relative path from local repo root.
		// - repoRoot = /git/root/
		// - configRoot = /git/root/repo/root/
		// - tokens[1] = repo/root/path/to/file.
		// The relative modified path to repo root would be "path/to/file"
		// TODO: need to test this.
		gitRootToRepoRoot, err := filepath.Rel(repoRoot, configRoot)
		if err != nil {
			log.Fatalf("unable to find relative path from base %v to %v, err %v", repoRoot, configRoot,
				err)
		}
		pathToRoot, err := filepath.Rel(gitRootToRepoRoot, tokens[1])
		if err != nil {
			log.Fatalf("unable to find relative path from repo root %v to %v, err %v",
				gitRootToRepoRoot, tokens[1], err)
		}
		ret = append(ret, config.ModifiedFile{
			Op:   tokens[0],
			Path: pathToRoot,
		})
	}

	return &ret
}

const (
	// allowedOusherBranch defines the branch which is allowed to push.
	allowedPusherBranch = "master"
)

type fileInfo struct {
	isDir   bool
	content []byte
}

// Pusher push the localRepo to etcd.
func Pusher(etcdConn *etcd.Client, root, etcdRoot string) {
	fmt.Printf("Push config from local dir <%s> to etcd dir <%s>\n", root, etcdRoot)

	checkGitVersion()

	// check if root is symbolic link and convert it
	if r, err := filepath.EvalSymlinks(root); err == nil {
		fmt.Printf("convert symbolic link root %s to %s\n", root, r)
		root = r
	}

	// cd to repo or fatal
	cdRepo(root)

	// get local repo root
	repoRoot := runSingleCmdOrFatal("git rev-parse --show-toplevel")
	fmt.Printf("local repo root: %s\n", repoRoot)

	// get repo name
	repo := path.Base(repoRoot)
	fmt.Printf("using repo: %s\n", repo)

	// get path to git root
	pathToRepo, err := filepath.Rel(repoRoot, root)
	if err != nil {
		log.Fatalf("unable to find relative path from <%s> to <%s>, err %s", repoRoot, root, err)
	}

	// get branch name
	branch := runSingleCmdOrFatal("git rev-parse --abbrev-ref HEAD")
	// TODO: may need to relax this constrain.
	/*
		if branch != allowedPusherBranch {
			log.Fatalf("only %s branch is allowed to push, whereas you currently are in %s", allowedPusherBranch, branch)
		}
	*/
	fmt.Printf("using branch: %s\n", branch)

	// get commit hash
	commitHash := runSingleCmdOrFatal("git rev-parse HEAD")
	fmt.Printf("using commit: %s\n", commitHash)

	// get timestamp, committer email and subject of a commit
	info := runSingleCmdOrFatal("git show --quiet --pretty=%at:%ce:%s " + commitHash)
	fmt.Printf("commit timestamp:email:subject: %s\n", info)
	tokens := strings.Split(info, ":")
	if len(tokens) < 3 {
		log.Fatalf("commit info is not in the timestamp:email:subject format:%s\n", info)
	}

	tsstr, email, subject := tokens[0], tokens[1], strings.Join(tokens[2:], ":")
	// convert tsstr to timestamp
	timestamp, _ := strconv.ParseInt(tsstr, 10, 64)

	// set etcd
	remoteRoot := strings.Trim(etcdRoot, " \t")
	if remoteRoot == "" {
		log.Fatalf("etcdRoot is empty\n")
	}
	infoPath := fmt.Sprintf(infoPrefix, remoteRoot)

	// check config root on etcd
	resp, err := etcdConn.Get(remoteRoot, true, false)
	if err != nil {
		log.Fatalf("error testing remoteRoot, %s: %s\n", remoteRoot, err)
	}
	log.Infof("remoteRoot %s verified\n", remoteRoot)

	// default etcdLastCommit is current repo newest commit
	prevCFG := &config.ConfigInfo{}
	etcdLastCommit := []byte(runSingleCmdOrFatal("git rev-list --max-parents=0 HEAD"))
	etcdHasCommit := false
	log.Infof("reading last pushed information for %s", infoPath)
	resp, err = etcdConn.Get(infoPath, true, false)
	if err == nil {
		jerr := json.Unmarshal([]byte(resp.Node.Value), prevCFG)
		if jerr == nil && prevCFG.Version != "" {
			// read previous commit
			etcdLastCommit = []byte(prevCFG.Version)
			etcdHasCommit = true
		}
	} else {
		// previos config is empty or invalid
		log.Infof("previous configInfo doesn't exist")
	}

	// TODO: empty etcdLastCommit cause diff-tree to only print files of current commit.
	filestr := runSingleCmdOrFatal(fmt.Sprintf("git diff-tree --no-commit-id --name-status -r %s %s %s", etcdLastCommit, commitHash, root))
	// filter out files that are descendents of the rootpath
	modFiles := filterRelatedFiles(root, repoRoot, filestr)
	if len(*modFiles) == 0 {
		promptUser("Remote repo seems to be already up-to-date "+
			"(remote commit %s vs local commit %s). Do you want to continue?",
			etcdLastCommit, commitHash)
	} else {
		fmt.Println("This push will notify the following file changes:")
		for _, m := range *modFiles {
			fmt.Printf("%+v\n", m)
		}
		fmt.Println("End of notify changes.")
	}

	if etcdHasCommit {
		fmt.Printf("Remote commit: %s\n",
			runSingleCmdOrFatal("git log --format=%h:%s(%aN) -n 1 "+string(etcdLastCommit)))
	}
	fmt.Printf("Local commit:  %s\n",
		runSingleCmdOrFatal("git log --format=%h:%s(%aN) -n 1 "+commitHash))
	fmt.Printf("Total commit(s) to be pushed: %s\n",
		runSingleCmdOrFatal("git rev-list --count "+commitHash+" ^"+string(etcdLastCommit)))
	fmt.Printf("Files changed between remote/local commits:\n%s\n",
		runSingleCmdOrFatal("git diff --name-only "+string(etcdLastCommit)+" "+commitHash+" "+root))
	promptUser("Ready to push?")

	// walk through the repo and save content
	content := make(map[string]fileInfo)

	// since Walk will pass in absolute path, we are going to take
	// the root out from the beginning
	sz := len(root)

	walkFunc := func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Fatalf("Unable to traverse file %s: %s", path, err)
		}

		// TODO: we no need to scan all the files in the localRepo. Just check the files which are tracked by git: git ls-files
		// ignore .xxx file
		base := filepath.Base(path)
		// skip hidden dir, e.g. .git
		if string(base[0]) == "." {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		bs := []byte{}
		// for dir we set "" content
		if !info.IsDir() {
			bs, err = ioutil.ReadFile(path)
			if err != nil {
				log.Fatalf("error reading file %s: %s\n", path, err)
			}
			bs = bytes.TrimRight(bs, "\n\r")
		}

		// skip root
		if path == root {
			return nil
		}
		// remove root path
		path = path[sz:]
		content[path] = fileInfo{isDir: info.IsDir(), content: bs}
		return nil
	}

	if err = filepath.Walk(root, walkFunc); err != nil {
		log.Fatalf("error when traversing %s: %s\n", root, err)
	}

	configInfo := config.ConfigInfo{
		Repo:    repo,
		Branch:  branch,
		Version: commitHash,
		Commit: config.CommitInfo{
			TimeStamp:      timestamp,
			CommitterEmail: email,
			Subject:        subject,
		},
		ModFiles:   *modFiles,
		PathToRepo: pathToRepo,
	}

	jsonBytes, err := json.Marshal(configInfo)
	if err != nil {
		log.Fatalf("error when marshal configInfo, err: %s", err)
	}
	fmt.Printf("ConfigInfo json: %s\n", string(jsonBytes[:len(jsonBytes)]))

	// Get previous pusher information from root node for sanity check.
	if !etcdHasCommit {
		promptUser("There is no existing commit in remote tree. It could because " +
			"you're pushing to a clean tree. Do you want to continue?")
	} else {
		// Sanity check of pusher info.
		if configInfo.Repo != prevCFG.Repo {
			promptUser("Repo to be pushed <%s> != remote repo name <%s>. Continue?",
				configInfo.Repo, prevCFG.Repo)
		}
		if configInfo.Branch != prevCFG.Branch {
			promptUser("Branch to be pushed <%s> != remote branch <%s>. Continue?",
				configInfo.Branch, prevCFG.Branch)
		}
		if configInfo.PathToRepo != prevCFG.PathToRepo {
			promptUser("Path to repo <%s> != remote path <%s>. Continue?",
				configInfo.PathToRepo, prevCFG.PathToRepo)
		}
	}

	keys := make([]string, len(content))
	// sort content's keys
	i := 0
	for k := range content {
		keys[i] = k
		i++
	}
	sort.Strings(keys)

	// set etcd content.
	for _, k := range keys {
		fileP := filepath.Join(remoteRoot, k)
		fmt.Printf("creating or setting %s\n", fileP)
		if content[k].isDir {
			if _, err := etcdConn.Get(fileP, true, false); err != nil {
				// dir doesn't exist
				resp, err = etcdConn.SetDir(fileP, 0)
			}
		} else {
			resp, err = etcdConn.Set(fileP, string(content[k].content), 0)
		}
		if err != nil {
			log.Fatalf("error when setting znode >%s(%s + %s)<. Config server will be inconsistent: %s",
				fileP, remoteRoot, k, err)
		}
	}

	// go over deletes in commit
	for _, mod := range *modFiles {
		if strings.ToLower(mod.Op) != "d" {
			continue
		}
		// it's a delete
		// Find the relative path to etcd root.
		fileP := filepath.Join(remoteRoot, mod.Path)
		fmt.Printf("deleting %s (%s + %s)\n", fileP, remoteRoot, mod.Path)
		_, err = etcdConn.Delete(fileP, true)
		if err != nil {
			log.Errorf("error deleting file >%s<. Will continue. error: %s\n", fileP, err)
		}
		// since git uses a file as  a commit unit, there is nothing to do with folder.
		// what we are going to do is to delete each fileP which is modified with "d" and its corresponding folder.
		// If we still have children under that folder, deleting the folder will fail but we do not care.
		pDir := filepath.Join(remoteRoot, path.Dir(mod.Path))
		_, err = etcdConn.DeleteDir(pDir)
		// In normal case, we should get an error.
		// so just logging, if we have no error here.
		if err == nil {
			log.Errorf("error deleting dir >%s<.\n", pDir)
		}
	}

	// touch the root node with commit info
	_, err = etcdConn.Set(infoPath, string(jsonBytes), 0)
	if err != nil {
		log.Fatalf("error setting remoteRoot >%s<: %s\n", remoteRoot, err)
	}
	fmt.Printf("All done\n")
}

// promptUser prompts user and fatal if user doesn't response "yes"
var promptUser = func(format string, args ...interface{}) {
	fmt.Printf(format+" [yes/NO]\n", args...)
	resp := ""
	_, err := fmt.Scanln(&resp)
	if err != nil {
		log.Fatalf("unable to scan user response, err %s", err)
	}
	if resp != "yes" {
		log.Fatalf("Exiting per user response <%s>.", resp)
	}
}

func main() {
	flag.Parse()

	ensembleStr := strings.Trim(*machines, " \t")
	if ensembleStr == "" {
		log.Fatalf("etcd machines is empty\n")
	}

	etcdConn := etcd.NewClient(strings.Split(ensembleStr, ","))
	if etcdConn == nil {
		log.Fatalf("error connecting to etcd machines, %s", *machines)
	}
	fmt.Printf("connected to etcd machines %s\n", ensembleStr)

	root := strings.TrimRight(strings.Trim(*rootpath, " \t"), "/")
	if root == "" {
		log.Fatalf("repo root path not set\n")
	}

	root, err := filepath.Abs(root)
	if err != nil {
		log.Fatalf("can't get absolute path for root %s: %s", root, err)
	}

	Pusher(etcdConn, root, *etcdroot)
	etcdConn.Close()
}
