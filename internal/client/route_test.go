package client

import (
	"fmt"
	"io"
	"net"
	"testing"
	"time"
)

func startEchoServer(t *testing.T) (addr string, cleanup func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				_, _ = io.Copy(conn, conn)
			}(c)
		}
	}()
	return ln.Addr().String(), func() { _ = ln.Close() }
}

func TestRelayTCP(t *testing.T) {
	echoAddr, stop := startEchoServer(t)
	defer stop()

	client, err := net.Dial("tcp", echoAddr)
	if err != nil {
		t.Fatal(err)
	}
	server, err := net.Dial("tcp", echoAddr)
	if err != nil {
		client.Close()
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		relayTCP(client, server)
		close(done)
	}()

	msg := []byte("relay-ping")
	if _, err := client.Write(msg); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(server, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != string(msg) {
		t.Fatalf("got %q want %q", buf, msg)
	}
	client.Close()
	<-done
}

func TestTCPForward(t *testing.T) {
	echoAddr, stop := startEchoServer(t)
	defer stop()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		for {
			in, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				out, err := net.Dial("tcp", echoAddr)
				if err != nil {
					return
				}
				defer out.Close()
				relayTCP(c, out)
			}(in)
		}
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	payload := []byte("forward-ok")
	if _, err := conn.Write(payload); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len(payload))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != string(payload) {
		t.Fatalf("got %q want %q", buf, payload)
	}
}

func TestSOCKS5ConnectIPv4(t *testing.T) {
	echoAddr, stop := startEchoServer(t)
	defer stop()
	host, portStr, _ := net.SplitHostPort(echoAddr)
	var port16 uint16
	_, _ = fmt.Sscanf(portStr, "%d", &port16)

	proxyLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer proxyLn.Close()
	go func() {
		for {
			c, err := proxyLn.Accept()
			if err != nil {
				return
			}
			go serveSOCKS5Client(c)
		}
	}()

	conn, err := net.Dial("tcp", proxyLn.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	if _, err := conn.Write([]byte{socksVer5, 0x01, socksAuthNone}); err != nil {
		t.Fatal(err)
	}
	resp := make([]byte, 2)
	if _, err := io.ReadFull(conn, resp); err != nil {
		t.Fatal(err)
	}
	if resp[0] != socksVer5 || resp[1] != socksAuthNone {
		t.Fatalf("auth resp: %v", resp)
	}

	ip := net.ParseIP(host).To4()
	req := []byte{socksVer5, socksCmdConnect, 0x00, socksAtypIPv4, ip[0], ip[1], ip[2], ip[3], byte(port16 >> 8), byte(port16)}
	if _, err := conn.Write(req); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 10)
	if _, err := io.ReadFull(conn, reply); err != nil {
		t.Fatal(err)
	}
	if reply[1] != 0x00 {
		t.Fatalf("connect failed code=%d", reply[1])
	}

	msg := []byte("socks-ok")
	if _, err := conn.Write(msg); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != string(msg) {
		t.Fatalf("got %q want %q", buf, msg)
	}
}

func TestSOCKS5ConnectDomain(t *testing.T) {
	echoAddr, stop := startEchoServer(t)
	defer stop()
	host, portStr, _ := net.SplitHostPort(echoAddr)
	var port16 uint16
	_, _ = fmt.Sscanf(portStr, "%d", &port16)

	proxyLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer proxyLn.Close()
	go func() {
		for {
			c, err := proxyLn.Accept()
			if err != nil {
				return
			}
			go serveSOCKS5Client(c)
		}
	}()

	conn, err := net.Dial("tcp", proxyLn.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	_, _ = conn.Write([]byte{socksVer5, 0x01, socksAuthNone})
	resp := make([]byte, 2)
	_, _ = io.ReadFull(conn, resp)

	dom := []byte(host)
	req := append([]byte{socksVer5, socksCmdConnect, 0x00, socksAtypDomain, byte(len(dom))}, dom...)
	req = append(req, byte(port16>>8), byte(port16))
	if _, err := conn.Write(req); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 10)
	if _, err := io.ReadFull(conn, reply); err != nil {
		t.Fatal(err)
	}
	if reply[1] != 0x00 {
		t.Fatalf("connect failed code=%d", reply[1])
	}

	msg := []byte("socks-domain")
	if _, err := conn.Write(msg); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != string(msg) {
		t.Fatalf("got %q want %q", buf, msg)
	}
}

func TestReadSocks5AddrIPv4(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	go func() {
		_, _ = server.Write([]byte{127, 0, 0, 1, 0x00, 0x50})
	}()

	host, port, err := readSocks5Addr(client, socksAtypIPv4)
	if err != nil {
		t.Fatal(err)
	}
	if host != "127.0.0.1" || port != 80 {
		t.Fatalf("got %s:%d", host, port)
	}
}

func TestReadSocks5AddrDomain(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	go func() {
		b := append([]byte{byte(len("example.com"))}, []byte("example.com")...)
		b = append(b, 0x01, 0xbb)
		_, _ = server.Write(b)
	}()

	host, port, err := readSocks5Addr(client, socksAtypDomain)
	if err != nil {
		t.Fatal(err)
	}
	if host != "example.com" || port != 443 {
		t.Fatalf("got %s:%d", host, port)
	}
}
