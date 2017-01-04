package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"path"
	"path/filepath"
	"time"
)

type Config struct {
	ListenOn, Origin string
	SecondsFast      int
}

var (
	config                 Config
	ListenAddr, OriginAddr *net.UDPAddr

	executableDir  = filepath.Dir(os.Args[0])
	configFilename = path.Join(executableDir, "config.json")
)

func loadData(filename string, v interface{}) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	return json.NewDecoder(f).Decode(v)
}

type Time struct {
	Seconds, Fractions uint32
}

type Message struct {
	Header [16]byte
	Times  [4]Time
}

func modifyMessage(message *Message, outgoing bool) {
	diff := config.SecondsFast
	if outgoing {
		diff = -diff
	}
	for i := 0; i < len(message.Times); i++ {
		if message.Times[i].Seconds != 0 {
			message.Times[i].Seconds += uint32(diff)
		}
	}
}

func modifyData(data []byte, outgoing bool) ([]byte, error) {
	message := new(Message)
	err := binary.Read(bytes.NewReader(data), binary.BigEndian, message)
	if err != nil {
		return nil, fmt.Errorf("failed to parse a message: %s", err)
	}

	modifyMessage(message, outgoing)

	buf := new(bytes.Buffer)
	binary.Write(buf, binary.BigEndian, *message)
	return buf.Bytes(), nil
}

const (
	Timeout        = 15 * time.Second
	MaxMessageSize = 1024
)

func handleQuery(clientConn *net.UDPConn, addr *net.UDPAddr, data []byte) error {
	log.Println("handling a query")

	data, err := modifyData(data, true)
	if err != nil {
		return err
	}

	serverConn, err := net.DialUDP("udp", nil, OriginAddr)
	if err != nil {
		return err
	}
	defer serverConn.Close()

	_, err = serverConn.Write(data)
	if err != nil {
		return err
	}

	serverConn.SetReadDeadline(time.Now().Add(Timeout))
	buf := make([]byte, MaxMessageSize)
	n, err := serverConn.Read(buf)
	if err != nil {
		return err
	}
	data = buf[:n]

	data, err = modifyData(data, false)
	if err != nil {
		return err
	}

	_, err = clientConn.WriteToUDP(data, addr)
	return err
}

func runHandleQuery(clientConn *net.UDPConn, addr *net.UDPAddr, data []byte) {
	err := handleQuery(clientConn, addr, data)
	if err != nil {
		log.Println(err)
	}
}

func loadConfig() error {
	err := loadData(configFilename, &config)
	if err != nil {
		return fmt.Errorf("can't load %s: %s", configFilename, err)
	}

	ListenAddr, err = net.ResolveUDPAddr("udp", config.ListenOn)
	if err != nil {
		return fmt.Errorf("can't resolve ListenOn address: %s", err)
	}

	OriginAddr, err = net.ResolveUDPAddr("udp", config.Origin)
	if err != nil {
		return fmt.Errorf("can't resolve Origin address: %s", err)
	}

	return nil
}

func main() {
	err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}

	conn, err := net.ListenUDP("udp", ListenAddr)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	log.Printf("listening on %s\n", config.ListenOn)

	buf := make([]byte, MaxMessageSize)
	for {
		n, addr, err := conn.ReadFromUDP(buf)
		if err != nil {
			log.Fatal(err)
		}

		data := append([]byte(nil), buf[:n]...)
		go runHandleQuery(conn, addr, data)
	}
}
