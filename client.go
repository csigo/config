package config

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-etcd/etcd"
	"github.com/facebookgo/stats"
	"github.com/sirupsen/logrus"
)

// Client provides a interface for configuration service
type Client interface {
	// ConfigInfo returns the ConfigInfo protobuf struct that
	// contains infomation w.r.t to repo and last commit
	ConfigInfo() ConfigInfo

	// Get request the content of file at path. If the path doesn't
	// exists then ok returns false
	Get(path string) ([]byte, error)

	// AddListener returns a chan that when any file that matches
	// the pathRegEx changes, the ModifiedFile will be sent over
	// through the channel
	AddListener(pathRegEx *regexp.Regexp) *chan ModifiedFile

	// Watch watches change of a file and invokes callback. It also invokes callback for the
	// first time and return error if there's one.
	Watch(path string, callback func([]byte) error, errChan chan<- error) error

	// RemoveListener remove a listener channel
	RemoveListener(ch *chan ModifiedFile)

	// List lists content under certain path
	List(path string) (map[string][]byte, error)

	// Stop is to stop client
	Stop() error
}

// NewClient return a new Client with option funtions.
// conn: a etcd client,
// root: the root path that contains the config tree
func NewClient(conn *etcd.Client, root string, options ...func(*clientImpl) error) (Client, error) {
	client := &clientImpl{
		etcdConn:  conn,
		root:      root,
		listeners: []*regexChan{},
		cache:     make(map[string][]byte),
		listers:   make(map[string]*lister),
		ctr:       dummyStat{},
	}

	// set option
	for _, opt := range options {
		if err := opt(client); err != nil {
			return nil, err
		}
	}

	// start config client
	err := client.init()
	return client, err
}

// Stat sets counter client
func Stat(counter stats.Client) func(*clientImpl) error {
	return func(c *clientImpl) error {
		if counter == nil {
			return errors.New("nil counter is not allowed")
		}
		c.ctr = counter
		return nil
	}
}

// NoMatchingLogs skip matching logs
func NoMatchingLogs() func(*clientImpl) error {
	return func(c *clientImpl) error {
		c.noMatchingLogs = true
		return nil
	}
}

// watchRetryTime defines the retry number of watch
var watchRetryTime = flag.Uint("csi_config_client_etcd_retry", uint(5), "etcd watch failed retry times, 0 for unlimited retry")

// fatalFn is used to test for watch
var fatalfFn = logrus.Fatalf

// regexChan ties up a Regexp with a channel, chan string
type regexChan struct {
	regex *regexp.Regexp
	ch    *chan ModifiedFile
}

// clientImpl implements the Client interface
type clientImpl struct {
	infoLock     sync.Mutex
	info         ConfigInfo
	etcdConn     *etcd.Client
	root         string
	listenerLock sync.Mutex
	listeners    []*regexChan
	cacheLock    sync.Mutex
	cache        map[string][]byte
	ctr          stats.Client
	// listers is a map from path to all listers.
	listers map[string]*lister
	// listerLock protects listers
	listerLock     sync.Mutex
	stopCh         chan bool
	noMatchingLogs bool
}

// init start monitoring the config file
func (c *clientImpl) init() error {
	c.stopCh = make(chan bool)
	// check if root exists
	_, err := c.etcdConn.Get(c.root, true, false)
	if err != nil {
		logrus.Errorf("etcd root<%s> doesn't exist, err: %s", c.root, err)
		return err
	}
	// start loop monitor
	infoPath := fmt.Sprintf(infoPrefix, c.root)
	logrus.Infof("start to monitor %s", infoPath)
	initErr := make(chan error)
	go c.loop(infoPath, initErr)
	return <-initErr
}

// loop starts to listen on configure info stored in storer, and
// starts dispatching to listeners
func (c *clientImpl) loop(infoPath string, initErr chan error) {
	// loop to listen to event
	retry := uint(0)
	resp := &etcd.Response{}
	firstTime, ok := true, false

	// get watch for config
	recv, err := c.getWatch(infoPath, c.stopCh)
	if err != nil {
		// the infor doesn't exist, but root is ok. so jump to listen change directly
		logrus.Infof("etcd root<%s> is clear, please push config first", c.root)
		initErr <- err
		return
	}

	for {
		select {
		case resp, ok = <-recv:
			if !ok {
				// watch error, retry
				retry++
				if *watchRetryTime > 0 && retry > *watchRetryTime {
					fatalfFn("retry watch failed over %v times", *watchRetryTime)
				}
				logrus.Errorf("watch etcd root<%s> failed, retry: %v", infoPath, retry)
				time.Sleep(time.Second)
				recv, _ = c.getWatch(infoPath, c.stopCh)
				continue
			}
			// watch successfully, set retry to 0
			retry = uint(0)
		case <-c.stopCh:
			// client stop
			logrus.Infof("config client receive stop signal")
			return
		}

		// unmarshal the config
		info, err := c.configMarshal(resp.Node.Value)

		// check if the config info is right for initializing
		if firstTime {
			if err != nil {
				// first time and config info is wrong
				initErr <- fmt.Errorf("etcd root config is wrong, err: %s", err)
				return
			}
			initErr <- nil
			firstTime = false
		}

		// empty cache
		for _, f := range info.ModFiles {
			logrus.Infof("delete %v from cache", f.Path)
			c.cacheLock.Lock()
			delete(c.cache, f.Path)
			c.cacheLock.Unlock()
		}

		// send file change event to listeners
		logrus.Infof("fire file change event on info: %v", info)
		c.fireFileChangeEvent(&info)
	}
}

func (c *clientImpl) getWatch(path string, stop chan bool) (chan *etcd.Response, error) {
	resp := make(chan *etcd.Response, 1)
	// get path
	r, err := c.etcdConn.Get(path, true, false)
	if err != nil {
		logrus.Errorf("etcd get %s failed, err: %s", path, err)
		close(resp)
		return resp, err
	}
	resp <- r
	// watch path
	go func() {
		c.ctr.BumpSum("config.watch.event", 1)
		logrus.Infof("starting watching %s for version %v", path, r.EtcdIndex+1)
		_, e := c.etcdConn.Watch(path, r.EtcdIndex+1, false, resp, stop)
		if e != nil && e != etcd.ErrWatchStoppedByUser {
			c.ctr.BumpSum("config.watch.err", 1)
			logrus.Infof("watch error:%s", e)
		}
	}()
	return resp, nil
}

// configMarshal returns ConfigInfo
func (c *clientImpl) configMarshal(s string) (ConfigInfo, error) {
	info := ConfigInfo{}
	err := json.Unmarshal([]byte(s), &info)
	if err != nil {
		// the content of node has problem
		c.ctr.BumpSum(cRootDataErr, 1)
		logrus.Errorf("can't unmarshal data: %v, error: %v", s, err)
		return info, err
	}
	return info, nil
}

// ConfigInfo returns the ConfigInfo protobuf struct that
// contains infomation w.r.t to repo and last commit
func (c *clientImpl) ConfigInfo() ConfigInfo {
	c.infoLock.Lock()
	defer c.infoLock.Unlock()
	return c.info
}

// Get request the content of file at path. If the path doesn't
// exists then ok returns false
func (c *clientImpl) Get(path string) ([]byte, error) {
	defer c.ctr.BumpTime(cGetProcTime).End()
	c.cacheLock.Lock()
	defer c.cacheLock.Unlock()
	// check cache
	if bytes, ok := c.cache[path]; ok {
		// cache hit
		return bytes, nil
	}

	c.ctr.BumpSum(cCacheMiss, 1)
	r, err := c.etcdConn.Get(filepath.Join(c.root, path), true, false)
	if err != nil {
		// get from etcd error
		return nil, err
	}
	c.cache[path] = []byte(r.Node.Value)
	return c.cache[path], nil
}

// List list the path tree under give path
func (c *clientImpl) List(path string) (map[string][]byte, error) {
	path = strings.Trim(path, "/")
	logrus.Infof("List trimmed path %s", path)
	c.listerLock.Lock()
	defer c.listerLock.Unlock()
	// if we alreay listed this path, just return it
	if l, ok := c.listers[path]; ok {
		return l.List(), nil
	}
	// otherwise, make a new lister and return.
	l, err := newLister(c, path)
	if err != nil {
		return nil, err
	}
	c.listers[path] = l
	return l.List(), nil
}

// AddListener returns a chan that when any file that matches
// the pathRegEx changes, the changed file will be sent over
// through the channel
// the listener only allow to lieten on the files that under root
// directory
func (c *clientImpl) AddListener(pathRegEx *regexp.Regexp) *chan ModifiedFile {
	c.listenerLock.Lock()
	defer c.listenerLock.Unlock()

	cn := make(chan ModifiedFile)
	c.listeners = append(c.listeners, &regexChan{regex: pathRegEx, ch: &cn})
	return &cn
}

func (c *clientImpl) Watch(
	path string,
	callback func([]byte) error,
	errChan chan<- error,
) error {
	// do is a helper function that gets a config path and invokes callback
	do := func() error {
		data, err := c.Get(path)
		if err != nil {
			return err
		}
		return callback(data)
	}

	// Invoke callback for initialization
	if err := do(); err != nil {
		return err
	}

	// Listen to specified path and invoke callback accordingly
	ch := c.AddListener(regexp.MustCompile(path))
	go func() {
		for range *ch {
			if err := do(); err != nil {
				if errChan != nil {
					errChan <- err
				}
			}
		}
	}()
	return nil
}

// RemoveListener remove a listener channel. RemoveListener will not call
// close on ch. However, after RemoveListener call, ch will no longer receive
// events
func (c *clientImpl) RemoveListener(ch *chan ModifiedFile) {
	c.listenerLock.Lock()
	defer c.listenerLock.Unlock()

	for i, regch := range c.listeners {
		if regch.ch == ch {
			c.listeners = append(c.listeners[:i], c.listeners[i+1:]...)
			break
		}
	}
}

// Stop is to stop client
func (c *clientImpl) Stop() error {
	close(c.stopCh)
	return nil
}

// fireFileChangeEvent is called whenever ConfigInfo.Files changes
func (c *clientImpl) fireFileChangeEvent(info *ConfigInfo) {
	c.infoLock.Lock()
	c.info = *info
	c.infoLock.Unlock()

	// need to clone listener as the callback could call AddListener/RemoveListener
	c.listenerLock.Lock()
	listeners := c.listeners[:]
	c.listenerLock.Unlock()

	for _, regch := range listeners {
		for _, f := range info.ModFiles {
			if !c.noMatchingLogs {
				logrus.Infof("Matching listener <%v> vs file <%v>", regch, f)
			}
			if regch.regex.Match([]byte(f.Path)) {
				logrus.Infof("Matched listener <%v> vs file <%v>", regch, f)
				*regch.ch <- f
			}
		}
	}
}
