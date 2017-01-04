package main

import (
	"bytes"
	"io/ioutil"
	"net"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"time"
)

var tcpResponsePrefixes = map[string]*regexp.Regexp{
	"smtp": regexp.MustCompile(`^220.*\n500`),
	"pop":  regexp.MustCompile(`^\+OK\r\n-ERR invalid command`),
	"http": regexp.MustCompile(`400 Bad Request|501 Not Implemented`),
}

func detectTCPService(conn *net.TCPConn) string {
	conn.SetWriteDeadline(time.Now().Add(Timeout))
	_, err := conn.Write([]byte(testRequest))
	if err != nil {
		return ""
	}
	err = conn.CloseWrite()
	if err != nil {
		return ""
	}

	conn.SetReadDeadline(time.Now().Add(Timeout))
	buf := make([]byte, 256)
	bytesRead := 0
	for bytesRead < len(buf) {
		n, err := conn.Read(buf[bytesRead:])
		if err != nil || n == 0 {
			break
		}
		bytesRead += n
	}
	if bytesRead == 0 {
		return ""
	}
	response := buf[:bytesRead]

	for protocol, expr := range tcpResponsePrefixes {
		if expr.Match(response) {
			return protocol
		}
	}
	return ""
}

var sntpClientRequest, dnsClientRequest []byte

const (
	SNTPServerResponseSize  = 48
	DNSServerResponseSubstr = "root-servers"
)

func loadRequestTemplates() error {
	executableDir := filepath.Dir(os.Args[0])

	data, err := ioutil.ReadFile(path.Join(executableDir, "sntp_client_request.dat"))
	if err != nil {
		return err
	}
	sntpClientRequest = data

	data, err = ioutil.ReadFile(path.Join(executableDir, "dns_client_request.dat"))
	if err != nil {
		return err
	}
	dnsClientRequest = data

	return nil
}

func checkUDPService(conn *net.UDPConn, request []byte) []byte {
	conn.SetWriteDeadline(time.Now().Add(Timeout))
	_, err := conn.Write(request)
	if err != nil {
		return nil
	}

	conn.SetReadDeadline(time.Now().Add(Timeout))
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if err != nil {
		return nil
	}
	return buf[:n]
}

func detectUDPService(conn *net.UDPConn) string {
	response := checkUDPService(conn, dnsClientRequest)
	if response != nil && bytes.Compare(response[:2], dnsClientRequest[:2]) == 0 &&
		bytes.Contains(response, []byte(DNSServerResponseSubstr)) {
		return "dns"
	}

	response = checkUDPService(conn, sntpClientRequest)
	if len(response) == SNTPServerResponseSize {
		return "sntp"
	}

	return ""
}
