package config

/* lister.go implements lister struct that lists all file names under specified path. */
import (
	"container/list"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/Sirupsen/logrus"
)

// lister implemnts config library's List() function.
type lister struct {
	client *clientImpl
	// path is the path to be listed.
	path string
	// tree stores all files under path and is updated automatically by listening to zk.
	tree map[string]struct{}
	// lock protect the tree
	lock sync.RWMutex
}

func newLister(client *clientImpl, path string) (*lister, error) {
	if client == nil {
		return nil, fmt.Errorf("empty config client")
	}
	if path == "" {
		return nil, fmt.Errorf("need to specify a related path")
	}

	//create a filepath instance of storer
	logrus.Infof("new storer filepath with root %v, path %v", client.root, path)
	ls := &lister{
		client: client,
		path:   strings.Trim(path, "/"),
	}
	//monitor the all dir/files that under the root
	escPath := regexp.QuoteMeta(path)
	regex, err := regexp.Compile("^" + escPath)
	if err != nil {
		return nil, fmt.Errorf("regexp compile error: %v", err)
	}
	// add listener to listen the change
	// if any modify on remote storer path, modify the tree in the storer filepath
	logrus.Infof("listening on %v, path %v, escaped path %v", regex, path, escPath)
	ch := ls.client.AddListener(regex)

	// build the tree first
	lsFunc := func(path string) ([]string, error) {
		resp, err := client.etcdConn.Get(path, true, false)
		if err != nil {
			return nil, err
		}
		c := []string{}
		for _, n := range resp.Node.Nodes {
			if n.Key != filepath.Base(infoPrefix) {
				// NOTE: in etcd, children are absolute path, so change path to relative path
				if f, e := filepath.Rel(path, n.Key); e == nil {
					c = append(c, f)
				}
			}
		}
		return c, nil
	}
	ls.tree, err = buildTree(client.root, path, lsFunc)
	if err != nil {
		return nil, fmt.Errorf("can't build tree. root %v path %v err %v", client.root, path, err)
	}
	go func() {
		for mod := range *ch {
			logrus.Infof("Op: %v, path: %v", mod.Op, mod.Path)
			//update the struct
			switch strings.ToUpper(mod.Op) {
			case "A":
				logrus.Info("add file path: %v", mod.Path)
				ls.lock.Lock()
				ls.tree[mod.Path] = struct{}{}
				ls.lock.Unlock()
			case "D":
				logrus.Info("delete file path: %v", mod.Path)
				ls.lock.Lock()
				delete(ls.tree, mod.Path)
				ls.lock.Unlock()
			}
		}
		logrus.Warning("leaving config service listener thread")
	}()
	return ls, nil
}

// List returns a snapshot of current tree strcuture
func (l *lister) List() map[string][]byte {
	l.lock.RLock()
	defer l.lock.RUnlock()
	t := make(map[string][]byte)
	for k := range l.tree {
		v, err := l.client.Get(k)
		if err != nil {
			l.client.ctr.BumpSum(cListGetFail, 1)
			logrus.Warningf("Skip path %v because can't get the content. Err %v", k, err)
			continue
		}
		t[k] = v
	}
	return t
}

// buildTree clone the whole tree in config service to storer filepath
var buildTree = func(
	confRoot string,
	treeRoot string,
	lsFunc func(path string) ([]string, error),
) (map[string]struct{}, error) {
	tree := make(map[string]struct{})
	queue := list.New()
	queue.PushBack(treeRoot)
	for e := queue.Front(); e != nil; e = e.Next() {
		path := e.Value.(string)
		path = strings.TrimPrefix(path, "/")

		//setting
		children, err := lsFunc(filepath.Join(confRoot, path))
		if err != nil {
			logrus.Errorf("get children from %v error: %v", path, err)
			return nil, err
		}
		// This is a file. Get the content and add to tree.
		if len(children) == 0 {
			tree[path] = struct{}{}
			continue
		}
		// This is a directory. Enqueue all children.
		for _, file := range children {
			queue.PushBack(filepath.Join(path, file))
		}
	}
	return tree, nil
}
