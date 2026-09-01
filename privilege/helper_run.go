package privilege

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"time"
)

func RunHelperIfRequested() bool {
	if len(os.Args) < 2 || os.Args[1] != HelperFlag {
		return false
	}
	if err := runHelper(os.Args[2:]); err != nil {
		fmt.Fprintf(os.Stderr, "disk-helper: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
	return true // unreachable
}

func runHelper(args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("invalid usage! %s <mode> [args...] <addr> <token>", HelperFlag)
	}
	token := args[len(args)-1]
	addr := args[len(args)-2]
	mode := args[0]
	extra := args[1 : len(args)-2]

	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("connecting back to parent: %w", err)
	}
	defer conn.Close()

	if _, err := fmt.Fprintf(conn, "%s\n", token); err != nil {
		return fmt.Errorf("sending handshake: %w", err)
	}

	switch mode {
	case "read":
		if len(extra) < 1 {
			return fmt.Errorf("read mode requires device path")
		}
		return helperStreamRead(conn, extra[0])
	case "duplicate":
		if len(extra) < 2 {
			return fmt.Errorf("duplicate mode requires device path and parent pid")
		}
		pid, err := strconv.Atoi(extra[1])
		if err != nil {
			return fmt.Errorf("invalid parent pid %q: %w", extra[1], err)
		}
		write := len(extra) >= 3 && extra[2] == "rw"
		return helperDuplicateHandle(conn, extra[0], pid, write)
	default:
		return fmt.Errorf("unknown helper mode %q", mode)
	}
}

func helperStreamRead(conn net.Conn, devicePath string) error {
	f, err := os.Open(devicePath)
	if err != nil {
		return fmt.Errorf("opening %q: %w", devicePath, err)
	}
	defer f.Close()

	var size int64
	if fi, err := f.Stat(); err == nil {
		size = fi.Size()
	}

	if size == 0 {
		if sz, err := deviceSizeForHelper(f); err == nil {
			size = sz
		}
	}

	var sizeBuf [8]byte
	binary.BigEndian.PutUint64(sizeBuf[:], uint64(size))
	if _, err := conn.Write(sizeBuf[:]); err != nil {
		return fmt.Errorf("writing size: %w", err)
	}

	_, err = io.Copy(conn, f)
	return err
}
