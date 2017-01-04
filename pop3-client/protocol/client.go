package protocol

import (
	"../message"
	"../utils"
	"bufio"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
)

type Command []string

func newReadWriter(stream io.ReadWriter) *bufio.ReadWriter {
	return bufio.NewReadWriter(bufio.NewReader(stream), bufio.NewWriter(stream))
}

type Client struct {
	host   string
	conn   net.Conn
	stream *bufio.ReadWriter

	Logger *log.Logger
}

const (
	okPrefix    = "+OK"
	errorPrefix = "-ERR"
)

func (client *Client) readResponse() (string, error) {
	line, err := utils.ReadLine(client.stream.Reader)
	if err != nil {
		return "", err
	}
	if client.Logger != nil {
		client.Logger.Printf("S: %s\n", line)
	}

	if strings.HasPrefix(line, okPrefix) {
		if len(line) == len(okPrefix) {
			line += " "
		}
		return line[len(okPrefix)+1:], nil
	}
	if strings.HasPrefix(line, errorPrefix) {
		if len(line) == len(errorPrefix) {
			line += " "
		}
		return "", fmt.Errorf("server returned an error: %s", line[len(errorPrefix)+1:])
	}
	return "", errors.New("server returned an unsupported response")
}

func (client *Client) readMultiLineResponse() ([]string, error) {
	response := make([]string, 0)
	for {
		line, err := utils.ReadLine(client.stream.Reader)
		if err != nil {
			return nil, err
		}
		if client.Logger != nil {
			client.Logger.Printf("S: %s\n", line)
		}

		if line == "." {
			break
		}
		response = append(response, line)
	}
	return response, nil
}

func (client *Client) execute(command Command) (string, error) {
	line := strings.Join(command, " ")
	if client.Logger != nil {
		client.Logger.Printf("C: %s\n", line)
	}

	err := utils.WriteLine(client.stream, line)
	if err != nil {
		return "", err
	}
	err = client.stream.Flush()
	if err != nil {
		return "", err
	}

	return client.readResponse()
}

func (client *Client) executeLong(command Command) (string, []string, error) {
	resp, err := client.execute(command)
	if err != nil {
		return "", nil, err
	}

	lines, err := client.readMultiLineResponse()
	if err != nil {
		return "", nil, err
	}

	return resp, lines, nil
}

func (client *Client) Connect(host string, port int) error {
	client.host = host

	var err error
	client.conn, err = net.Dial("tcp", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		return err
	}

	client.stream = newReadWriter(client.conn)

	_, err = client.readResponse()
	if err != nil {
		return err
	}

	_, resp, err := client.executeLong([]string{"CAPA"})
	if err != nil {
		return err
	}
	tlsSupported := false
	for _, cap := range resp {
		if strings.EqualFold(cap, "STLS") {
			tlsSupported = true
			break
		}
	}
	if tlsSupported {
		_, err = client.execute([]string{"STLS"})
		if err != nil {
			return err
		}
		client.conn = tls.Client(client.conn, &tls.Config{ServerName: client.host})
		client.stream = newReadWriter(client.conn)
	}
	return nil
}

func (client *Client) Close() error {
	_, quitErr := client.execute([]string{"QUIT"})
	closeErr := client.conn.Close()

	if quitErr != nil {
		return quitErr
	}
	return closeErr
}

func (client *Client) Login(login, password string) error {
	_, err := client.execute([]string{"USER", login})
	if err != nil {
		return err
	}

	_, err = client.execute([]string{"PASS", password})
	return err
}

type ScanListing struct {
	Id, Size int
}

func (client *Client) List() ([]ScanListing, error) {
	_, lines, err := client.executeLong([]string{"LIST"})
	if err != nil {
		return nil, err
	}

	result := make([]ScanListing, 0)
	for _, line := range lines {
		var item ScanListing
		fmt.Sscan(line, &item.Id, &item.Size)
		result = append(result, item)
	}
	return result, nil
}

func unescapeBody(lines []string) {
	for i, line := range lines {
		if strings.HasPrefix(line, ".") {
			lines[i] = line[1:]
		}
	}
}

func (client *Client) getMessage(command Command) (*message.Entity, error) {
	_, lines, err := client.executeLong(command)
	if err != nil {
		return nil, err
	}

	msg, err := message.FromLines(lines)
	if err != nil {
		return nil, err
	}
	unescapeBody(msg.Body)
	return msg, nil
}

func (client *Client) Top(id int, count int) (*message.Entity, error) {
	return client.getMessage([]string{"TOP", strconv.Itoa(id), strconv.Itoa(count)})
}

func (client *Client) Retr(id int) (*message.Entity, error) {
	return client.getMessage([]string{"RETR", strconv.Itoa(id)})
}
