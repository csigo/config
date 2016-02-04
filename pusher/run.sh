#!/bin/bash

go run main.go \
  -csi.config.repo.root="$HOME/workspace/htc/config-test/cg/sta"\
  -csi.config.etcd.root="/configs" \
  -csi.config.etcd.machines="http://127.0.0.1:5566"
