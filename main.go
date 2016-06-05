package main

import (
	"./protocol"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

var config struct {
	ListenOn string
}

var (
	executableDir  = filepath.Dir(os.Args[0])
	configFilename = path.Join(executableDir, "config.json")
)

func loadData(filename string, v interface{}) error {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return err
	}

	err = json.Unmarshal(data, v)
	if err != nil {
		return err
	}
	return nil
}

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

func handleClient(clientConn net.Conn) error {
	defer clientConn.Close()

	request := new(protocol.Request)
	err := request.ReadFrom(clientConn)
	if err != nil {
		return err
	}
	// TODO: Send error depending on error type

	if request.Method == "CONNECT" {
		return errors.New("CONNECT method is not implemented")
	}
	var addr string
	addr, request.Url, err = transformURL(request.Url)
	if err != nil {
		return err
	}

	serverConn, err := net.Dial("tcp", addr)
	if err != nil {
		return err
	}
	defer serverConn.Close()

	err = request.WriteTo(serverConn)
	if err != nil {
		return err
	}

	response := new(protocol.Response)
	err = response.ReadFrom(serverConn)
	if err != nil {
		return err
	}

	return response.WriteTo(clientConn)
}

func runHandleClient(clientConn net.Conn) {
	err := handleClient(clientConn)
	if err != nil {
		log.Println("error on handling a client:", err)
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
