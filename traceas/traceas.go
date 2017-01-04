package main

import (
	"bufio"
	"fmt"
	"gopkg.in/alecthomas/kingpin.v2"
	"log"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

var IPRegexp = regexp.MustCompile(`^\s*(\d+) .* \((\d+\.\d+\.\d+\.\d+)\)`)

const UnknownLabel = "unknown"

var host = kingpin.Arg("host", "Host").Required().String()

var privateIPSegments = []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}

func isPublic(ip string) bool {
	addr := net.ParseIP(ip)
	if !addr.IsGlobalUnicast() {
		return false
	}
	for _, segment := range privateIPSegments {
		_, subnet, _ := net.ParseCIDR(segment)
		if subnet.Contains(addr) {
			return false
		}
	}
	return true
}

func getComments(ip string) string {
	if !isPublic(ip) {
		return "private network"
	}

	asInfo, _ := getASInfo(ip)

	asnRepr := UnknownLabel
	if asInfo.Number != 0 {
		asnRepr = strconv.Itoa(asInfo.Number)
	}
	country := UnknownLabel
	if asInfo.Country != "" {
		country = asInfo.Country
	}
	providerDescr := UnknownLabel
	if len(asInfo.ProviderDescr) > 0 {
		providerDescr = strings.Join(asInfo.ProviderDescr, ", ")
	}

	return fmt.Sprintf("ASN: %s, country: %s, provider: %s", asnRepr, country, providerDescr)
}

func main() {
	kingpin.CommandLine.Help = "Trace route to host and show AS information"
	kingpin.Parse()

	cmd := exec.Command("traceroute", *host)
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		match := IPRegexp.FindStringSubmatch(line)
		if match != nil {
			ip := match[2]
			fmt.Printf("%2s  %s (%s)\n", match[1], ip, getComments(ip))
		} else {
			fmt.Println(line)
		}
	}
	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}
}
