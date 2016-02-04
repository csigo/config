/*Package config implements a configuration server using git and zookeeper.

Besides maintaining structured data describing how systems should work together,
a good configuration server needs to have the following characteristics:

1. Version control. Should be able to revert to a previous version shall there be any
error after a push.
2. Fast, simple, and reliable notification to thousands or tens of thousands servers.
3. Flexible to describe complex systems.
4. Ease of use

For the above reasons, we chose to implement our config system with two components that
are already widely used with a rock solid foundation: git, and zookeeper.

It is easy to see that git can handle require 1, 3, and 4, while zookeeper satisfies 2, and 4.
The system works as follows:

1. Configurations are stored in a git repo. You can have one file for the whole systems, or you
can have hundreds or thousands of files each detailing a part of the whole system. It
doesn't matter. The config system deal with repo, and its commits. The sysadmin maintain
the repo using whatever git tool he/she choose. Changes to the git repo are not saved
to config system. So just use it as a normal repo, branch, edit, etc as you would for a normal
repo.

2. For changes to be pushed to our config system, you need to call csi/service/config/pusher/pusher. pusher works on the HEAD of the repo. Whatever you
have checked out, when pusher is called, will be pushed onto config system. pusher will
update data in a zookeeper ensemble that acts as the persistent, and notification layer.
When pusher is called, it is given a root path, which is the root path for the configuration
information. This way, it is ok if you just dedicate a subdirectory of your repo for configuration,
instead of dedicating a completely new one. All descendent files of the root directory will
be saved onto zookeeper, e.g. a/b/c/d will be set in zookeeper as znode a/b/c/d where
the content of znode a/b/c/d is the content of file a/b/c/d. Additionally, pusher will collect
repo and commit info, encode them in a json structure and store it in the root znode for
this configuration system. The json structure contains repo name, branch name, last commit
hash, committer email, timestamp, and a files modified in the last commit.

3. The information stored in step 2 on the root znode is important. It allows a client of the
configuration system to just listen to one znode, and gets notified of not only a change has
happened, but what file(s) got changed, and decided whether or not it should update.
This job is done by csi/service/config/client.go. Client.go allows one to register to receive
event on any file change.

4. Pusher requires "git" to be in the PATH, as it calls git related command extensively.
The following is a list of git commands that pusher uses to find out various repo related info:

# show repo name
00:57 $ basename `git rev-parse --show-toplevel`
csi

# show HEAD branch name
00:56 $ git rev-parse --abbrev-ref HEAD
master

# show HEAD commit hash
00:54 $ git rev-parse HEAD
6e390e716805599f8f694a5f654649d92ec638f6

# show files affected in a commit
01:02 $ git diff-tree --no-commit-id --name-status -r 6e390e716805599f8f694a5f654649d92ec638f6
common/types/user/login_record.pb.go
external/oauth/cache.go
make_go.sh
pp/frontend/main.go
.....

# get timestamp, committer email and subject of a commit
03:03 $ git show --pretty="format:%at:%ce:%s" 6e390e716805599f8f694a5f654649d92ec638f6 | head -n 1
1394199814:alien_lien@htc.com:Add 1) Account Manager and 2) User Management Service

5. Client can listen on the configuration change with a regex string that specifies which
files it is interested. It will be notified when any of files that matches the regex has
changed. Note: this is achieved by regex match the changed files in the commit log with
is published to the root znode.

*/
package config
