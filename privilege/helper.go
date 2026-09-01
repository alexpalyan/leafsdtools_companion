package privilege

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"time"
)

const HelperFlag = "--disk-helper"

func startElevatedHelperConn(mode string, extraArgs ...string) (net.Conn, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolving own executable: %w", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("opening local listener: %w", err)
	}

	token, err := randomToken()
	if err != nil {
		ln.Close()
		return nil, fmt.Errorf("generating handshake token: %w", err)
	}

	args := append([]string{HelperFlag, mode}, extraArgs...)
	args = append(args, ln.Addr().String(), token)

	if err := relaunchElevatedHelperPlatform(exe, args); err != nil {
		ln.Close()
		return nil, fmt.Errorf("requesting elevated permission: %w", err)
	}

	conn, err := acceptWithTimeout(ln, 90*time.Second)
	if err != nil {
		return nil, err
	}

	if err := verifyHandshake(conn, token); err != nil {
		conn.Close()
		return nil, err
	}

	return conn, nil
}

func openElevatedDeviceReadViaHelperStream(devicePath string) (io.ReadCloser, int64, error) {
	conn, err := startElevatedHelperConn("read", devicePath)
	if err != nil {
		return nil, 0, err
	}

	var sizeBuf [8]byte
	if _, err := io.ReadFull(conn, sizeBuf[:]); err != nil {
		conn.Close()
		return nil, 0, fmt.Errorf("reading device size from helper: %w", err)
	}

	return conn, int64(binary.BigEndian.Uint64(sizeBuf[:])), nil
}

func acceptWithTimeout(ln net.Listener, timeout time.Duration) (net.Conn, error) {
	defer ln.Close()

	type result struct {
		conn net.Conn
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		conn, err := ln.Accept()
		ch <- result{conn, err}
	}()

	select {
	case res := <-ch:
		if res.err != nil {
			return nil, fmt.Errorf("waiting for elevated helper: %w", res.err)
		}
		return res.conn, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("timed out waiting for elevated permission (the prompt may have been dismissed or is still pending)")
	}
}

func verifyHandshake(conn net.Conn, token string) error {
	line := make([]byte, len(token)+1)
	if _, err := io.ReadFull(conn, line); err != nil {
		return fmt.Errorf("helper handshake failed: %w", err)
	}
	if string(line[:len(token)]) != token || line[len(token)] != '\n' {
		return fmt.Errorf("helper handshake token mismatch")
	}
	return nil
}

func randomToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
