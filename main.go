package main

import (
	"./protocol"
	"errors"
	"fmt"
	"log"
	"net"
	"net/url"
	"strings"
)

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
	clientTextConn := &protocol.TextConn{Conn: clientConn}

	request := new(protocol.Request)
	err := request.ReadFrom(clientTextConn)
	if err != nil {
		return err
	}
	// TODO: Send error depending on error type

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
	serverTextConn := &protocol.TextConn{Conn: serverConn}

	err = request.WriteTo(serverTextConn)
	if err != nil {
		return err
	}

	response := new(protocol.Response)
	err = response.ReadFrom(serverTextConn)
	if err != nil {
		return err
	}

	return response.WriteTo(clientTextConn)
}

func runHandleClient(clientConn net.Conn) {
	err := handleClient(clientConn)
	if err != nil {
		log.Println("error on handling a client:", err)
	}
}

func main() {
	log.Printf("listening on 8080\n")
	ln, err := net.Listen("tcp", ":8080")
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
