package client

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"time"
)

const (
	socksVer5       = 0x05
	socksAuthNone   = 0x00
	socksCmdConnect = 0x01
	socksAtypIPv4   = 0x01
	socksAtypDomain = 0x03
)

func serveSOCKS5Client(c net.Conn) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(60 * time.Second))

	buf := make([]byte, 258)
	if _, err := io.ReadAtLeast(c, buf[:2], 2); err != nil {
		return
	}
	if buf[0] != socksVer5 {
		return
	}
	nMethods := int(buf[1])
	if _, err := io.ReadFull(c, buf[:nMethods]); err != nil {
		return
	}
	if _, err := c.Write([]byte{socksVer5, socksAuthNone}); err != nil {
		return
	}
	if _, err := io.ReadFull(c, buf[:4]); err != nil {
		return
	}
	if buf[0] != socksVer5 || buf[1] != socksCmdConnect {
		_, _ = c.Write([]byte{socksVer5, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	host, port, err := readSocks5Addr(c, buf[3])
	if err != nil {
		return
	}
	target := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	remote, err := net.DialTimeout("tcp", target, 15*time.Second)
	if err != nil {
		log.Printf("socks dial %s: %v", target, err)
		_, _ = c.Write([]byte{socksVer5, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer remote.Close()
	_, _ = c.Write([]byte{socksVer5, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
	_ = c.SetDeadline(time.Time{})
	relayTCP(c, remote)
}

func readSocks5Addr(c net.Conn, atyp byte) (string, int, error) {
	switch atyp {
	case socksAtypIPv4:
		buf := make([]byte, 4+2)
		if _, err := io.ReadFull(c, buf); err != nil {
			return "", 0, err
		}
		host := net.IP(buf[:4]).String()
		port := int(binary.BigEndian.Uint16(buf[4:6]))
		return host, port, nil
	case socksAtypDomain:
		var lenBuf [1]byte
		if _, err := io.ReadFull(c, lenBuf[:]); err != nil {
			return "", 0, err
		}
		n := int(lenBuf[0])
		buf := make([]byte, n+2)
		if _, err := io.ReadFull(c, buf); err != nil {
			return "", 0, err
		}
		host := string(buf[:n])
		port := int(binary.BigEndian.Uint16(buf[n : n+2]))
		return host, port, nil
	default:
		return "", 0, fmt.Errorf("unsupported atyp %d", atyp)
	}
}

func relayTCP(a, b net.Conn) {
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(b, a)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(a, b)
		done <- struct{}{}
	}()
	<-done
}
