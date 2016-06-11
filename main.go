package main

import (
	"./protocol"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/andybalholm/cascadia"
	"html/template"
	"io"
	"log"
	"net"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const ServerName = "http-proxy"

type Config struct {
	ListenOn, AllowTunnelsTo string
	RemoveElements           map[string][]string
}

type URLRule struct {
	Pattern   *regexp.Regexp
	Selectors []string
}

var (
	config                  Config
	allowedTunnelAddrRegexp *regexp.Regexp
	urlRules                []URLRule

	executableDir  = filepath.Dir(os.Args[0])
	configFilename = path.Join(executableDir, "config.json")
	templateDir    = path.Join(executableDir, "templates")
)

func loadData(filename string, v interface{}) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	return json.NewDecoder(f).Decode(v)
}

func loadTemplate(name string) *template.Template {
	path := path.Join(templateDir, name)
	result, err := template.New(name).ParseFiles(path)
	if err != nil {
		log.Fatalf("failed to load %s: %s\n", path, err)
	}
	return result
}

var errorTemplate = loadTemplate("error.tpl")

func transformURL(rawURL string) (string, string, error) {
	url, err := url.Parse(rawURL)
	if err != nil {
		return "", "", err
	}
	if url.Opaque != "" || url.User != nil || url.Fragment != "" {
		return "", "", errors.New(fmt.Sprintf(`URL %s contains prohibited elements`, rawURL))
	}

	host := url.Host
	if !strings.ContainsRune(host, ':') {
		host += ":80"
	}
	url.Scheme = ""
	url.Host = ""
	return host, url.String(), nil
}

func defaultResponseHeaders() []protocol.Header {
	date := strings.Replace(time.Now().UTC().Format(time.RFC1123), "UTC", "GMT", 1)
	return []protocol.Header{
		{"Server", ServerName},
		{"Date", date},
	}
}

func copyAndClose(dst *net.TCPConn, src *net.TCPConn) {
	written, err := io.Copy(dst, src)
	src.CloseRead()
	dst.CloseWrite()
	if err != nil {
		log.Println("error on a tunnel: " + err.Error())
	} else {
		log.Printf("tunnel side closed, %d bytes copied from %s to %s\n", written,
			src.RemoteAddr(), dst.RemoteAddr())
	}
}

func handleTunnel(clientConn *net.TCPConn, serverConn *net.TCPConn) error {
	response := &protocol.Response{
		Protocol: "HTTP/1.1",
		Code:     protocol.StatusOK,
		Reason:   protocol.StatusText[protocol.StatusOK],
		MessageBase: protocol.MessageBase{
			Headers: defaultResponseHeaders(),
		},
	}
	err := response.WriteTo(clientConn)
	if err != nil {
		return err
	}
	log.Printf("established tunnel between %s and %s\n", clientConn.RemoteAddr(), serverConn.RemoteAddr())

	go copyAndClose(clientConn, serverConn)
	copyAndClose(serverConn, clientConn)
	return nil
}

var tunnelAddrSchemeRegexp = regexp.MustCompile(`(.+):\d+`)

func tunnelAddrAllowed(addr string) bool {
	match := tunnelAddrSchemeRegexp.FindStringSubmatch(addr)
	if match == nil {
		return false
	}
	ips, err := net.LookupIP(match[1])
	if err != nil {
		return false
	}
	for _, ip := range ips {
		if !ip.IsGlobalUnicast() {
			return false
		}
	}

	return allowedTunnelAddrRegexp.MatchString(addr)
}

func handleClient(clientConn net.Conn) (*protocol.Error, bool) {
	request := new(protocol.Request)
	err := request.ReadFrom(clientConn)
	if err != nil {
		return &protocol.Error{protocol.StatusBadRequest, err}, false
	}

	url := strings.TrimSpace(request.Url)
	var addr string
	if request.Method == protocol.MethodConnect {
		addr = request.Url
		if !tunnelAddrAllowed(addr) {
			return &protocol.Error{protocol.StatusForbidden,
				errors.New("This address isn't allowed for CONNECT")}, false
		}
	} else {
		addr, request.Url, err = transformURL(request.Url)
		if err != nil {
			return &protocol.Error{protocol.StatusBadRequest, err}, false
		}
	}

	serverConn, err := net.Dial("tcp", addr)
	if err != nil {
		return &protocol.Error{protocol.StatusBadGateway, err}, false
	}
	if request.Method == protocol.MethodConnect {
		err := handleTunnel(clientConn.(*net.TCPConn), serverConn.(*net.TCPConn))
		if err != nil {
			return &protocol.Error{0, err}, false
		}
		return nil, true
	}
	defer serverConn.Close()

	err = request.WriteTo(serverConn)
	if err != nil {
		return &protocol.Error{protocol.StatusBadGateway, err}, false
	}

	response := new(protocol.Response)
	err = response.ReadFrom(serverConn)
	if err != nil {
		return &protocol.Error{protocol.StatusBadGateway, err}, false
	}
	err = ModifyResponse(url, response)
	if err != nil {
		return &protocol.Error{protocol.StatusBadGateway, err}, false
	}

	err = response.WriteTo(clientConn)
	if err != nil {
		return &protocol.Error{0, err}, false
	}
	return nil, false
}

func sendErrorResponse(clientConn net.Conn, protocolErr *protocol.Error) error {
	reason := protocol.StatusText[protocolErr.Status]
	data := struct {
		Status     int
		Reason     string
		Error      error
		ServerName string
		Config
	}{
		Status:     protocolErr.Status,
		Reason:     reason,
		Error:      protocolErr.Error,
		ServerName: ServerName,
		Config:     config,
	}

	response := &protocol.Response{
		Protocol: "HTTP/1.0",
		Code:     protocolErr.Status,
		Reason:   reason,
		MessageBase: protocol.MessageBase{
			Headers: append(defaultResponseHeaders(),
				protocol.Header{"Content-Type", "text/html"}),
			Body: protocol.NewPipe(),
		},
	}
	response.SetChunked(false)
	go func() { response.Body.Writer.CloseWithError(errorTemplate.Execute(response.Body.Writer, data)) }()
	return response.WriteTo(clientConn)
}

func runHandleClient(clientConn net.Conn) {
	var keepConn bool
	defer func() {
		if !keepConn {
			clientConn.Close()
		}
	}()

	var protocolErr *protocol.Error
	protocolErr, keepConn = handleClient(clientConn)
	if protocolErr != nil {
		log.Printf("error on handling a client (%d): %s\n", protocolErr.Status, protocolErr.Error)
		if protocolErr.Status != 0 {
			err := sendErrorResponse(clientConn, protocolErr)
			if err != nil {
				log.Println("error on sending a error response: " + err.Error())
			}
		}
	}
}

func loadConfig() error {
	err := loadData(configFilename, &config)
	if err != nil {
		return fmt.Errorf("can't load %s: %s", configFilename, err)
	}

	allowedTunnelAddrRegexp, err = regexp.Compile(config.AllowTunnelsTo)
	if err != nil {
		return fmt.Errorf("can't compile a regexp from AllowTunnelsTo: %s", err)
	}

	for expr, selectors := range config.RemoveElements {
		pattern, err := regexp.Compile(expr)
		if err != nil {
			return fmt.Errorf("can't compile a regexp from RemoveElements: %s", err)
		}
		for _, selector := range selectors {
			_, err := cascadia.Compile(selector)
			if err != nil {
				return fmt.Errorf("can't compile a CSS selector: %s", err)
			}
		}
		urlRules = append(urlRules, URLRule{pattern, selectors})
	}

	log.Println("config checked")
	return nil
}

func main() {
	err := loadConfig()
	if err != nil {
		log.Fatalln(err)
	}

	log.Printf("listening on %s\n", config.ListenOn)
	ln, err := net.Listen("tcp", config.ListenOn)
	if err != nil {
		log.Fatal("listen failed:", err.Error())
	}
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Fatal("accept failed:", err.Error())
		}
		go runHandleClient(conn)
	}
}
