package main

import (
	"./protocol"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const ServerName = "http-proxy"

type Config struct {
	ListenOn string
}

var (
	config Config

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

func handleClient(clientConn net.Conn) *protocol.Error {
	request := new(protocol.Request)
	err := request.ReadFrom(clientConn)
	if err != nil {
		return &protocol.Error{protocol.StatusBadRequest, err}
	}

	if request.Method == "CONNECT" {
		return &protocol.Error{protocol.StatusNotImplemented, errors.New("CONNECT method is not implemented")}
	}
	var addr string
	addr, request.Url, err = transformURL(request.Url)
	if err != nil {
		return &protocol.Error{protocol.StatusBadRequest, err}
	}

	serverConn, err := net.Dial("tcp", addr)
	if err != nil {
		return &protocol.Error{protocol.StatusBadGateway, err}
	}
	defer serverConn.Close()

	err = request.WriteTo(serverConn)
	if err != nil {
		return &protocol.Error{protocol.StatusBadGateway, err}
	}

	response := new(protocol.Response)
	err = response.ReadFrom(serverConn)
	if err != nil {
		return &protocol.Error{protocol.StatusBadGateway, err}
	}

	err = response.WriteTo(clientConn)
	if err != nil {
		return &protocol.Error{0, err}
	}
	return nil
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
	body := new(bytes.Buffer)
	err := errorTemplate.Execute(body, data)
	if err != nil {
		return err
	}

	date := strings.Replace(time.Now().UTC().Format(time.RFC1123), "UTC", "GMT", 1)
	response := &protocol.Response{
		Protocol: "HTTP/1.0",
		Code:     protocolErr.Status,
		Reason:   reason,
		MessageBase: protocol.MessageBase{
			Headers: []protocol.Header{
				{"Server", ServerName},
				{"Date", date},
				{"Content-Type", "text/html"},
				{"Content-Length", "0"}, // WriteTo will recalculate it
			},
			BodyReader: body,
		},
	}
	return response.WriteTo(clientConn)
}

func runHandleClient(clientConn net.Conn) {
	defer clientConn.Close()

	protocolErr := handleClient(clientConn)
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

func main() {
	err := loadData(configFilename, &config)
	if err != nil {
		log.Fatalf("can't load %s\n", configFilename)
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
