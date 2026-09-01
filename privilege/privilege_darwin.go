//go:build darwin

package privilege

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/sys/unix"
)

func isElevatedPlatform() bool {
	return os.Geteuid() == 0
}

func openElevatedDevicePlatform(devicePath string, write bool) (io.ReadWriteCloser, int64, error) {
	absPath := devicePath
	if !strings.HasPrefix(absPath, "/") {
		return nil, 0, fmt.Errorf("device path is not absolute: %q", devicePath)
	}

	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err != nil {
		return nil, 0, fmt.Errorf("creating socketpair for authopen: %w", err)
	}
	parentFile := os.NewFile(uintptr(fds[0]), "authopen-parent")
	childFile := os.NewFile(uintptr(fds[1]), "authopen-child")

	args := []string{"-stdoutpipe"}
	if write {
		args = append(args, "-o", "2") // O_RDWR
	}
	args = append(args, absPath)

	cmd := exec.Command("/usr/libexec/authopen", args...)
	cmd.Stdout = childFile
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		parentFile.Close()
		childFile.Close()
		return nil, 0, fmt.Errorf("starting authopen: %w", err)
	}
	childFile.Close()

	fd, gotFD, recvErr := recvFD(parentFile)
	parentFile.Close()
	waitErr := cmd.Wait()

	if recvErr != nil {
		return nil, 0, fmt.Errorf("receiving device handle from authopen: %w", recvErr)
	}
	if !gotFD {
		if waitErr != nil {
			return nil, 0, fmt.Errorf("authopen declined to open %q (denied, or Full Disk Access isn't granted): %w", absPath, waitErr)
		}
		return nil, 0, fmt.Errorf("authopen did not hand back a device handle for %q", absPath)
	}

	f := os.NewFile(uintptr(fd), absPath)
	var size int64
	if fi, statErr := f.Stat(); statErr == nil {
		size = fi.Size()
	}
	return f, size, nil
}

func recvFD(conn *os.File) (fd int, ok bool, err error) {
	genericConn, err := net.FileConn(conn)
	if err != nil {
		return 0, false, err
	}
	defer genericConn.Close()

	unixConn, isUnix := genericConn.(*net.UnixConn)
	if !isUnix {
		return 0, false, fmt.Errorf("unexpected connection type from authopen")
	}

	buf := make([]byte, 4)
	oob := make([]byte, unix.CmsgSpace(4))
	_, oobn, _, _, err := unixConn.ReadMsgUnix(buf, oob)
	if err != nil {
		return 0, false, err
	}
	if oobn == 0 {
		return 0, false, nil
	}

	scms, err := unix.ParseSocketControlMessage(oob[:oobn])
	if err != nil || len(scms) == 0 {
		return 0, false, err
	}
	fds, err := unix.ParseUnixRights(&scms[0])
	if err != nil || len(fds) == 0 {
		return 0, false, err
	}
	return fds[0], true, nil
}

func relaunchElevatedHelperPlatform(exe string, args []string) error {
	parts := append([]string{exe}, args...)
	quoted := make([]string, len(parts))
	for i, p := range parts {
		quoted[i] = "'" + strings.ReplaceAll(p, "'", `'\''`) + "'"
	}
	shellCmd := strings.Join(quoted, " ")
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(shellCmd)
	script := fmt.Sprintf(`do shell script "%s" with administrator privileges`, escaped)
	return exec.Command("osascript", "-e", script).Start()
}
