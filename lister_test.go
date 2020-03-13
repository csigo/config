package config

import (
	"fmt"
	"log"
	"path/filepath"
	"testing"
	"time"

	"github.com/coreos/go-etcd/etcd"
	"github.com/csigo/test"
	"github.com/stretchr/testify/assert"
)

type mockLister struct {
	depth int
}

func (l *mockLister) List(path string) ([]string, error) {
	if path == "nonExist" {
		return []string{}, fmt.Errorf("non exist test")
	}
	if l.depth >= 2 {
		return []string{}, nil
	}
	l.depth++
	return []string{"a", "b"}, nil
}

func TestBuildTreeNormal(t *testing.T) {
	l := &mockLister{depth: 0}
	tree, err := buildTree("root", "/", l.List)
	assert.NoError(t, err)
	assert.Equal(t, 3, len(tree))

	fmt.Println(tree)
	_, ok := tree["b"]
	assert.Equal(t, true, ok)

	_, ok = tree["a/a"]
	assert.Equal(t, true, ok)

	_, ok = tree["a/b"]
	assert.Equal(t, true, ok)
}

func TestBuildTreeEmpty(t *testing.T) {
	l := &mockLister{depth: 0}
	_, err := buildTree("", "nonExist", l.List)
	assert.Error(t, err)
}

func TestNewLister(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}
	// create etcd
	fmt.Println("starting etcd")
	srvLaunch := test.NewServiceLauncher()
	port, stop, err := srvLaunch.Start(test.Etcd)
	if err != nil {
		log.Fatalf("etcd failed, %s", err)
	}
	assert.NoError(t, err)
	etcdEnsemble := fmt.Sprintf("http://localhost:%s", port)
	etcdCli := etcd.NewClient([]string{etcdEnsemble})
	assert.NotNil(t, etcdCli)
	fmt.Println("started etcd")

	// create config client
	root := "/root"
	_, err = etcdCli.SetDir(root, 0)
	assert.NoError(t, err)
	_, err = etcdCli.Set(fmt.Sprintf(infoPrefix, root), "{}", 0)
	assert.NoError(t, err)
	tcli, err := NewClient(etcdCli, root)
	cli, ok := tcli.(*clientImpl)
	assert.True(t, ok)

	// build file tree
	_, err = etcdCli.SetDir(filepath.Join(root, "a/b"), 0)
	assert.NoError(t, err)

	// create lister
	ls, err := newLister(cli, "a")
	assert.NoError(t, err)

	// add file in dir
	_, err = etcdCli.Set(fmt.Sprintf(infoPrefix, root), `{"modfiles":[{"op":"A", "path":"a/b/f1"}]}`, 0)
	assert.NoError(t, err)
	time.Sleep(time.Second)
	_, ok = ls.tree["a/b/f1"]
	assert.True(t, ok)

	// delete file in dir
	_, err = etcdCli.Set(fmt.Sprintf(infoPrefix, root), `{"modfiles":[{"op":"D", "path":"a/b/f1"}]}`, 0)
	assert.NoError(t, err)
	time.Sleep(time.Second)
	_, ok = ls.tree["a/b/f1"]
	assert.False(t, ok)

	assert.NoError(t, cli.Stop())
	assert.NoError(t, stop())
}
