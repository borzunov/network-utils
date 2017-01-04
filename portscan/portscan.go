package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"golang.org/x/net/icmp"
	"gopkg.in/alecthomas/kingpin.v2"
	"log"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	DefaultDescriptorCount = 10
	DesiredDescriptorLimit = 70000
	Timeout                = 3 * time.Second
)

var descriptorLimit int

func loadDescriptorLimit() {
	var actualLimit syscall.Rlimit
	err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &actualLimit)
	if err != nil {
		log.Fatal("failed to get rlimit:", err)
	}

	desiredLimit := syscall.Rlimit{Cur: DesiredDescriptorLimit, Max: DesiredDescriptorLimit}
	err = syscall.Setrlimit(syscall.RLIMIT_NOFILE, &desiredLimit)
	if err == nil {
		actualLimit = desiredLimit
	} else {
		desiredLimit = syscall.Rlimit{Cur: actualLimit.Max, Max: actualLimit.Max}
		err = syscall.Setrlimit(syscall.RLIMIT_NOFILE, &desiredLimit)
		if err == nil {
			actualLimit = desiredLimit
		}
	}
	if desiredLimit.Cur != DesiredDescriptorLimit {
		log.Printf("failed to set desired rlimit (%d concurrent connections are available)", actualLimit.Cur)
	}

	descriptorLimit = int(actualLimit.Cur)
}

type PortRange struct {
	Min, Max int
}

type PortInfo struct {
	Number int
	Protocol string
}

func checkTCPPort(ip string, port int) (*string, error) {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", ip, port), Timeout)
	if err != nil {
		net_err, ok := err.(net.Error)
		if !(ok && net_err.Timeout()) && !strings.Contains(err.Error(), "connection refused") {
			return nil, err
		}
	} else {
		protocol := detectTCPService(conn.(*net.TCPConn))

		conn.Close()
		return &protocol, nil
	}
	return nil, nil
}

type PortCollection []PortInfo

func (p PortCollection) Len() int           { return len(p) }
func (p PortCollection) Less(i, j int) bool { return p[i].Number < p[j].Number }
func (p PortCollection) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

func scanTCPPorts(ip string, portRange PortRange) []PortInfo {
	openedPorts := make(chan PortInfo, portRange.Max-portRange.Min+1)
	sem := make(chan struct{}, descriptorLimit-DefaultDescriptorCount)
	var wg sync.WaitGroup
	for port := portRange.Min; port <= portRange.Max; port++ {
		wg.Add(1)
		go func(port int) {
			sem <- struct{}{}

			protocol, err := checkTCPPort(ip, port)
			if err != nil {
				log.Fatal("failed to check a TCP port:", err)
			}
			if protocol != nil {
				openedPorts <- PortInfo{port, *protocol}
			}

			<-sem
			wg.Done()
		}(port)
	}
	wg.Wait()
	close(openedPorts)

	result := make([]PortInfo, 0, len(openedPorts))
	for info := range openedPorts {
		result = append(result, info)
	}
	sort.Sort(PortCollection(result))
	return result
}

const (
	ProtocolICMP = 1
	ProtocolUDP  = 17
)

func listenForUnreachablePorts(conn *icmp.PacketConn, dstIP string, portRange PortRange) (map[int]struct{}, error) {
	closedPorts := make(map[int]struct{})

	buf := make([]byte, 1024)
	conn.SetReadDeadline(time.Now().Add(Timeout))
	for {
		bufsize, addr, err := conn.ReadFrom(buf)
		if err != nil {
			net_err, ok := err.(net.Error)
			if ok && net_err.Timeout() {
				break
			}
			return nil, err
		}
		if addr.String() != dstIP {
			continue
		}

		message, err := icmp.ParseMessage(ProtocolICMP, buf[:bufsize])
		if err != nil {
			log.Println("failed to parse an ICMP message:", err.Error())
			continue
		}
		body, ok := message.Body.(*icmp.DstUnreach)
		if !ok {
			continue
		}

		if message.Code != 3 {
			return nil, errors.New(fmt.Sprintf("Destination unreachable (code %d)\n", message.Code))
		}
		sourceData := body.Data

		header, err := icmp.ParseIPv4Header(sourceData)
		if err != nil {
			log.Println("failed to parse an IPv4 header in an ICMP message:", err.Error())
			continue
		}
		if header.Protocol != ProtocolUDP || len(sourceData) < header.Len+4 {
			continue
		}
		dstPort := int(binary.BigEndian.Uint16(sourceData[header.Len+2 : header.Len+4]))
		if !(portRange.Min <= dstPort && dstPort <= portRange.Max) {
			continue
		}
		closedPorts[dstPort] = struct{}{}

		conn.SetReadDeadline(time.Now().Add(Timeout))
	}
	return closedPorts, nil
}

const UDPMaxOpenedCount = 100

var testRequest = []byte("hello123\n")

func makeUDPRequests(ip string, portRange PortRange) {
	for port := portRange.Min; port <= portRange.Max; port++ {
		conn, err := net.Dial("udp", fmt.Sprintf("%s:%d", ip, port))
		if err != nil {
			log.Fatal("can't create a UDP socket:", err)
		}
		_, err = conn.Write(testRequest)
		if err != nil {
			log.Fatal("can't send a UDP packet:", err)
		}
		conn.Close()
	}
}

func scanUDPPorts(ip string, portRange PortRange) []PortInfo {
	conn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		log.Fatal("failed to start capturing ICMP packets:", err)
	}
	defer conn.Close()

	go makeUDPRequests(ip, portRange)

	closedPorts, err := listenForUnreachablePorts(conn, ip, portRange)
	if err != nil {
		log.Fatal("failed on capturing ICMP packets:", err)
	}

	openedCount := portRange.Max - portRange.Min + 1 - len(closedPorts)
	if openedCount > UDPMaxOpenedCount {
		log.Printf(`found %d opened ports (it seems like the host filters ICMP "Port Unreachable" messages)`,
			openedCount)
		return nil
	}
	log.Printf("detecting service on %d opened ports", openedCount)

	protocols := make(map[int]string)
	protocolMutex := &sync.Mutex{}
	var wg sync.WaitGroup
	for port := portRange.Min; port <= portRange.Max; port++ {
		if _, closed := closedPorts[port]; closed {
			continue
		}

		wg.Add(1)
		go func(port int) {
			conn, err := net.Dial("udp", fmt.Sprintf("%s:%d", ip, port))
			if err != nil {
				log.Fatal("can't create a UDP socket:", err)
			}
			defer conn.Close()

			protocol := detectUDPService(conn.(*net.UDPConn))
			if protocol != "" {
				protocolMutex.Lock()
				protocols[port] = protocol
				protocolMutex.Unlock()
			}

			wg.Done()
		}(port)
	}
	wg.Wait()

	result := make([]PortInfo, 0)
	for port := portRange.Min; port <= portRange.Max; port++ {
		if _, closed := closedPorts[port]; closed {
			continue
		}
		result = append(result, PortInfo{port, protocols[port]})
	}
	return result
}

func makeReport(scanIP func(string, PortRange) []PortInfo, ip string, portRange PortRange, protocol string) {
	if portRange.Min == 0 || portRange.Min > portRange.Max {
		return
	}

	log.Printf("scanning %s ports", protocol)
	openedPorts := scanIP(ip, portRange)
	if openedPorts == nil {
		return
	}

	for _, info := range openedPorts {
		fmt.Printf("%s/%d opened", protocol, info.Number)
		if info.Protocol != "" {
			fmt.Printf(" (%s)", info.Protocol)
		}
		fmt.Println()
	}
	closedCount := portRange.Max-portRange.Min+1-len(openedPorts)
	if closedCount > 0 {
		log.Printf("found %d closed %s ports", closedCount, protocol)
	}
}

func parseIntArg(arg string) int {
	value, err := strconv.Atoi(arg)
	if err != nil {
		log.Fatalf("\"%s\" is not an integer\n", arg)
	}
	return value
}

func parsePortRange(portRange string) PortRange {
	if portRange == "none" {
		return PortRange{}
	}
	if portRange == "all" {
		return PortRange{1, 65535}
	}

	parts := strings.Split(portRange, "-")
	if len(parts) > 2 {
		log.Fatal(`Too much "-" in a port range`)
	}

	min := parseIntArg(parts[0])
	if len(parts) == 1 {
		return PortRange{min, min}
	}
	return PortRange{min, parseIntArg(parts[1])}
}

var host = kingpin.Arg("host", "Host").Required().String()
var tcpRange = kingpin.Flag("tcp", "TCP port range (inclusive)").Default("all").String()
var udpRange = kingpin.Flag("udp", "UDP port range (inclusive)").Default("none").String()

func main() {
	kingpin.CommandLine.Help = "Scan TCP and UDP ports of a host"
	kingpin.Parse()

	err := loadRequestTemplates()
	if err != nil {
		log.Fatal("can't find a resource file:", err)
	}
	loadDescriptorLimit()

	addrs, err := net.LookupHost(*host)
	if err != nil {
		log.Fatal("failed to lookup host:", err)
	}
	ip := addrs[0]
	log.Printf("processing %s", ip)

	makeReport(scanTCPPorts, ip, parsePortRange(*tcpRange), "tcp")
	makeReport(scanUDPPorts, ip, parsePortRange(*udpRange), "udp")
}
