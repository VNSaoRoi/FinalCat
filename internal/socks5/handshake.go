package socks5

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"
)

const (
	Ver5       = 0x05
	AuthNone   = 0x00
	CmdConnect = 0x01
	AtypIPv4   = 0x01
	AtypDomain = 0x03
)

// Negotiate performs SOCKS5 auth + CONNECT and returns the target dial address.
func Negotiate(c net.Conn) (host string, port int, err error) {
	_ = c.SetDeadline(time.Now().Add(60 * time.Second))
	buf := make([]byte, 258)
	if _, err = io.ReadAtLeast(c, buf[:2], 2); err != nil {
		return
	}
	if buf[0] != Ver5 {
		err = fmt.Errorf("unsupported socks version %d", buf[0])
		return
	}
	nMethods := int(buf[1])
	if _, err = io.ReadFull(c, buf[:nMethods]); err != nil {
		return
	}
	if _, err = c.Write([]byte{Ver5, AuthNone}); err != nil {
		return
	}
	if _, err = io.ReadFull(c, buf[:4]); err != nil {
		return
	}
	if buf[0] != Ver5 || buf[1] != CmdConnect {
		err = fmt.Errorf("unsupported command %d", buf[1])
		_ = ReplyFail(c, 0x07)
		return
	}
	host, port, err = readAddr(c, buf[3])
	if err != nil {
		return
	}
	_ = c.SetDeadline(time.Time{})
	return
}

func readAddr(c net.Conn, atyp byte) (string, int, error) {
	switch atyp {
	case AtypIPv4:
		buf := make([]byte, 6)
		if _, err := io.ReadFull(c, buf); err != nil {
			return "", 0, err
		}
		return net.IP(buf[:4]).String(), int(binary.BigEndian.Uint16(buf[4:6])), nil
	case AtypDomain:
		var n [1]byte
		if _, err := io.ReadFull(c, n[:]); err != nil {
			return "", 0, err
		}
		buf := make([]byte, int(n[0])+2)
		if _, err := io.ReadFull(c, buf); err != nil {
			return "", 0, err
		}
		host := string(buf[:int(n[0])])
		port := int(binary.BigEndian.Uint16(buf[int(n[0]):]))
		return host, port, nil
	default:
		return "", 0, fmt.Errorf("unsupported atyp %d", atyp)
	}
}

func ReplyOK(c net.Conn) error {
	_, err := c.Write([]byte{Ver5, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
	return err
}

func ReplyFail(c net.Conn, code byte) error {
	_, err := c.Write([]byte{Ver5, code, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
	return err
}
