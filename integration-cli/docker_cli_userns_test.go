// +build !windows

package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/docker/docker/integration-cli/checker"
	"github.com/docker/docker/pkg/stringid"
	"github.com/docker/docker/pkg/system"
	"github.com/go-check/check"
	"gotest.tools/assert"
)

// user namespaces test: run daemon with remapped root setting
// 1. validate uid/gid maps are set properly
// 2. verify that files created are owned by remapped root
func (s *DockerDaemonSuite) TestDaemonUserNamespaceRootSetting(c *check.C) {
	testRequires(c, DaemonIsLinux, testEnv.IsLocalDaemon, UserNamespaceInKernel)

	s.d.StartWithBusybox(c, "--userns-remap", "default")

	tmpDir, err := ioutil.TempDir("", "userns")
	assert.NilError(c, err)

	defer os.RemoveAll(tmpDir)

	// Set a non-existent path
	tmpDirNotExists := path.Join(os.TempDir(), "userns"+stringid.GenerateRandomID())
	defer os.RemoveAll(tmpDirNotExists)

	// we need to find the uid and gid of the remapped root from the daemon's root dir info
	uidgid := strings.Split(filepath.Base(s.d.Root), ".")
	c.Assert(uidgid, checker.HasLen, 2, check.Commentf("Should have gotten uid/gid strings from root dirname: %s", filepath.Base(s.d.Root)))
	uid, err := strconv.Atoi(uidgid[0])
	assert.NilError(c, err, "Can't parse uid")
	gid, err := strconv.Atoi(uidgid[1])
	assert.NilError(c, err, "Can't parse gid")

	// writable by the remapped root UID/GID pair
	assert.NilError(c, os.Chown(tmpDir, uid, gid))

	out, err := s.d.Cmd("run", "-d", "--name", "userns", "-v", tmpDir+":/goofy", "-v", tmpDirNotExists+":/donald", "busybox", "sh", "-c", "touch /goofy/testfile; top")
	assert.NilError(c, err, "Output: %s", out)

	user := s.findUser(c, "userns")
	c.Assert(uidgid[0], checker.Equals, user)

	// check that the created directory is owned by remapped uid:gid
	statNotExists, err := system.Stat(tmpDirNotExists)
	assert.NilError(c, err)
	c.Assert(statNotExists.UID(), checker.Equals, uint32(uid), check.Commentf("Created directory not owned by remapped root UID"))
	c.Assert(statNotExists.GID(), checker.Equals, uint32(gid), check.Commentf("Created directory not owned by remapped root GID"))

	pid, err := s.d.Cmd("inspect", "--format={{.State.Pid}}", "userns")
	c.Assert(err, checker.IsNil, check.Commentf("Could not inspect running container: out: %q", pid))
	// check the uid and gid maps for the PID to ensure root is remapped
	// (cmd = cat /proc/<pid>/uid_map | grep -E '0\s+9999\s+1')
	_, err = RunCommandPipelineWithOutput(
		exec.Command("cat", "/proc/"+strings.TrimSpace(pid)+"/uid_map"),
		exec.Command("grep", "-E", fmt.Sprintf("0[[:space:]]+%d[[:space:]]+", uid)))
	assert.NilError(c, err)

	_, err = RunCommandPipelineWithOutput(
		exec.Command("cat", "/proc/"+strings.TrimSpace(pid)+"/gid_map"),
		exec.Command("grep", "-E", fmt.Sprintf("0[[:space:]]+%d[[:space:]]+", gid)))
	assert.NilError(c, err)

	// check that the touched file is owned by remapped uid:gid
	stat, err := system.Stat(filepath.Join(tmpDir, "testfile"))
	assert.NilError(c, err)
	c.Assert(stat.UID(), checker.Equals, uint32(uid), check.Commentf("Touched file not owned by remapped root UID"))
	c.Assert(stat.GID(), checker.Equals, uint32(gid), check.Commentf("Touched file not owned by remapped root GID"))

	// use host usernamespace
	out, err = s.d.Cmd("run", "-d", "--name", "userns_skip", "--userns", "host", "busybox", "sh", "-c", "touch /goofy/testfile; top")
	c.Assert(err, checker.IsNil, check.Commentf("Output: %s", out))
	user = s.findUser(c, "userns_skip")
	// userns are skipped, user is root
	c.Assert(user, checker.Equals, "root")
}

// findUser finds the uid or name of the user of the first process that runs in a container
func (s *DockerDaemonSuite) findUser(c *check.C, container string) string {
	out, err := s.d.Cmd("top", container)
	c.Assert(err, checker.IsNil, check.Commentf("Output: %s", out))
	rows := strings.Split(out, "\n")
	if len(rows) < 2 {
		// No process rows founds
		c.FailNow()
	}
	return strings.Fields(rows[1])[0]
}
