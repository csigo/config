package main

import (
	"flag"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/coreos/go-etcd/etcd"
	"github.com/csigo/config"
)

var (
	etcdRoot = flag.String("csi.config.etcd.root", "", "etcd root dir for config info")
	machines = flag.String("csi.config.etcd.machines", "", "etcd machines in http://host:port,host:port,... format")
	regexstr = flag.String("regex", "", "regex string that matches files to be notified.")
)

func main() {
	flag.Parse()

	// set etcd
	root := strings.Trim(*etcdRoot, " \t")
	if root == "" {
		log.Fatalf("etcdRoot is empty\n")
	}

	machinesStr := strings.Trim(*machines, " \t")
	if machinesStr == "" {
		log.Fatalf("etcd machines is empty\n")
	}

	etcdConn := etcd.NewClient(strings.Split(machinesStr, ","))
	if etcdConn == nil {
		log.Fatalf("error connecting to etcd machines, %s", machines)
	}
	fmt.Printf("connected to etcd machines %s\n", machinesStr)

	if *regexstr == "" {
		log.Fatalf("regex is empty\n")
	}

	regex, err := regexp.Compile(*regexstr)
	if err != nil {
		log.Fatalf("regex string %s is not valid: %s\n", regexstr, err)
	}

	// counter pkgname and prefix should be empty
	client, err := config.NewClient(etcdConn, root)
	if err != nil {
		log.Fatalf("error creating config client:%s\n", err)
	}

	ch := client.AddListener(regex)

	for {
		mod := <-*ch

		info := client.ConfigInfo()

		tm := time.Unix(info.Commit.TimeStamp, 0)

		fmt.Printf("Repo: %s, Branch: %s, Commit: %s, Who: %s, When: %d, Subject: %s\n",
			info.Repo, info.Branch, info.Version, info.Commit.CommitterEmail,
			tm.Format("Jan 2, 2006 at 3:04pm (MST)"), info.Commit.Subject)
		switch strings.ToLower(mod.Op) {
		case "d":
			fmt.Printf("file %s deleted\n", mod.Path)
		case "a":
			fmt.Printf("file %s added.", mod.Path)
			bs, err := client.Get(mod.Path)
			if err != nil {
				fmt.Printf(" error: %s\n", err)
			} else {
				fmt.Printf(" Content:\n%s\n", string(bs[:len(bs)]))
			}
		case "m":
			fmt.Printf("file %s modified.", mod.Path)
			bs, err := client.Get(mod.Path)
			if err != nil {
				fmt.Printf(" error: %s\n", err)
			} else {
				fmt.Printf(" Content:\n%s\n", string(bs[:len(bs)]))
			}
		default:
			fmt.Printf("Unknown op: %v\n", mod)
		}
	}
}
