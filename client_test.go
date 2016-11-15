package config

import (
	"fmt"
	"log"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/coreos/go-etcd/etcd"
	"github.com/csigo/portforward"
	"github.com/csigo/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

func TestConfigClientTestSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}
	suite.Run(t, new(ConfigClientTestSuite))
}

type ConfigClientTestSuite struct {
	suite.Suite

	srvLaunch test.ServiceLauncher
	srvPort   int
	srvStop   func() error
	etcdCli   *etcd.Client

	root string
}

func (c *ConfigClientTestSuite) SetupSuite() {
	fmt.Println("starting etcd")
	c.srvLaunch = test.NewServiceLauncher()
	port, stop, err := c.srvLaunch.Start(test.Etcd)
	c.srvStop = stop
	c.srvPort = port
	if err != nil {
		log.Fatalf("etcd failed, %s", err)
	}
	assert.NoError(c.T(), err)
	etcdEnsemble := fmt.Sprintf("http://localhost:%d", port)
	c.etcdCli = etcd.NewClient([]string{etcdEnsemble})
	assert.NotNil(c.T(), c.etcdCli)
	fmt.Println("started etcd")
}

func (c *ConfigClientTestSuite) TearDownSuite() {
	c.srvStop()
}

func (c *ConfigClientTestSuite) SetupTest() {
	c.root = "/test"
	c.etcdCli.SetDir(c.root, 0)
	c.etcdCli.Set(fmt.Sprintf(infoPrefix, c.root), "{}", 0)
}

func (c *ConfigClientTestSuite) TearDownTest() {
	_, err := c.etcdCli.Delete(c.root, true)
	assert.NoError(c.T(), err)
}

func (c *ConfigClientTestSuite) TestNewClientFail() {
	_, err := NewClient(c.etcdCli, "CATBUG")
	assert.Error(c.T(), err)
}

func (c *ConfigClientTestSuite) TestNewClientClearTree() {
	_, err := c.etcdCli.Delete(fmt.Sprintf(infoPrefix, c.root), true)
	assert.NoError(c.T(), err)
	_, err = NewClient(c.etcdCli, c.root)
	assert.Error(c.T(), err)
}

func (c *ConfigClientTestSuite) TestNewClientInitConfigFail() {
	c.etcdCli.Set(fmt.Sprintf(infoPrefix, c.root), "[this is an invalid json string]", 0)
	_, err := NewClient(c.etcdCli, c.root)
	assert.Error(c.T(), err)
}

func (c *ConfigClientTestSuite) TestNewWatchFailed() {
	// new etcd client with port forwarding
	port := 23123
	root := "/watch"
	stop, err := portforward.PortForward(fmt.Sprintf("localhost:%d", port), fmt.Sprintf("localhost:%d", c.srvPort))
	assert.NoError(c.T(), err)
	etcdCli := etcd.NewClient([]string{fmt.Sprintf("http://localhost:%d", port)})
	assert.NotNil(c.T(), etcdCli)
	_, err = etcdCli.SetDir(root, 0)
	assert.NoError(c.T(), err)

	ch := make(chan struct{}, 1)
	fatalfFn = func(s string, iface ...interface{}) {
		ch <- struct{}{}
		logrus.Infof("Called fatalf:%s", fmt.Sprintf(s, iface...))
	}

	etcdCli.Set(fmt.Sprintf(infoPrefix, root), "{}", 0)
	cli, err := NewClient(etcdCli, root)
	assert.NoError(c.T(), err)

	// sleep for 1 sec and disconnect etcd
	time.Sleep(time.Second)
	close(stop)

	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		c.T().Fail()
	}
	// close client and wait stopping all routines absolutely
	cli.Stop()
	time.Sleep(5 * time.Second)
	fatalfFn = logrus.Fatalf
}

func (c *ConfigClientTestSuite) TestGet() {
	// set file and create client
	path := "/file"
	value := "testget"
	c.etcdCli.Set(filepath.Join(c.root, path), value, 0)
	cli, err := NewClient(c.etcdCli, c.root)
	assert.NoError(c.T(), err)

	// get file
	b, err := cli.Get(path)
	assert.NoError(c.T(), err)
	assert.NotNil(c.T(), b)
	assert.Equal(c.T(), value, string(b))

	// get file again to improve coverage
	b, err = cli.Get(path)
	assert.NoError(c.T(), err)
	assert.NotNil(c.T(), b)
	assert.Equal(c.T(), value, string(b))
	cli.Stop()
}

func (c *ConfigClientTestSuite) TestGetFail() {
	// set file and create client
	cli, err := NewClient(c.etcdCli, c.root)
	assert.NoError(c.T(), err)
	// get file
	path := "/file"
	b, err := cli.Get(path)
	assert.Error(c.T(), err)
	assert.Nil(c.T(), b)
	cli.Stop()
}

func (c *ConfigClientTestSuite) TestList() {
	// set file and create client
	c.etcdCli.SetDir(filepath.Join(c.root, "a"), 0)
	c.etcdCli.Set(filepath.Join(c.root, "a/b"), "list_b", 0)
	c.etcdCli.Set(filepath.Join(c.root, "a/c"), "list_c", 0)
	cli, err := NewClient(c.etcdCli, c.root)
	assert.NoError(c.T(), err)

	// first time
	dirs, err := cli.List("a")
	_, ok := dirs["a/b"]
	assert.True(c.T(), ok)
	_, ok = dirs["a/c"]
	assert.True(c.T(), ok)

	// test twice to improve coverage of cache
	dirs, err = cli.List("a")
	_, ok = dirs["a/b"]
	assert.True(c.T(), ok)
	_, ok = dirs["a/c"]
	assert.True(c.T(), ok)
	cli.Stop()
}

func (c *ConfigClientTestSuite) TestListener() {
	// set file and create client
	tcli, err := NewClient(c.etcdCli, c.root)
	assert.NoError(c.T(), err)
	cli, _ := tcli.(*clientImpl)

	// add listener
	reg, err := regexp.Compile("^test")
	assert.NoError(c.T(), err)
	listenCh := cli.AddListener(reg)
	cf := &ConfigInfo{ModFiles: []ModifiedFile{ModifiedFile{Op: "test", Path: "test/ok"}}}
	go cli.fireFileChangeEvent(cf)
	// listening should succeed
	f := <-*listenCh
	assert.Equal(c.T(), f.Path, "test/ok")
	assert.Equal(c.T(), f.Op, "test")

	// remove listener and change file again
	cli.RemoveListener(listenCh)
	go cli.fireFileChangeEvent(cf)
	select {
	case <-*listenCh:
		// should not receive anythins coz the listener is removed
		c.T().Fail()
	case <-time.After(time.Second):
	}
	cli.Stop()
}

func (c *ConfigClientTestSuite) TestWatch() {
	// set file and create client
	tcli, err := NewClient(c.etcdCli, c.root)
	assert.NoError(c.T(), err)
	cli, _ := tcli.(*clientImpl)

	ch := make(chan []byte, 1)
	callback := func(val []byte) error {
		ch <- val
		return nil
	}

	// watch should return error if there's no value in path
	c.Error(cli.Watch("test/ok", callback, nil))

	path := "test/ok"
	value := "testGet"
	c.etcdCli.Set(filepath.Join(c.root, path), value, 0)
	c.NoError(cli.Watch("test/ok", callback, nil))
	cf := &ConfigInfo{ModFiles: []ModifiedFile{ModifiedFile{Op: "test", Path: "/test/ok"}}}
	go cli.fireFileChangeEvent(cf)

	c.Equal("testGet", string(<-ch))
	c.Equal("testGet", string(<-ch))
}
