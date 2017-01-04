package protocol

import (
	"../message"
	"../utils"
	"bufio"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
)

type Command []string

type Response struct {
	Code  int
	Lines []string
}

func newReadWriter(stream io.ReadWriter) *bufio.ReadWriter {
	return bufio.NewReadWriter(bufio.NewReader(stream), bufio.NewWriter(stream))
}

type Client struct {
	host   string
	conn   net.Conn
	stream *bufio.ReadWriter

	Logger *log.Logger
}

const codeDigitCount = 3

func (client *Client) readResponse() (*Response, error) {
	response := new(Response)
	var line string
	for {
		var err error
		line, err = utils.ReadLine(client.stream.Reader)
		if err != nil {
			return nil, err
		}
		if client.Logger != nil {
			client.Logger.Printf("S: %s\n", line)
		}

		if len(line) < codeDigitCount {
			return nil, errors.New("too short response line")
		}
		if len(line) == codeDigitCount {
			line += " "
		}
		response.Lines = append(response.Lines, line[codeDigitCount+1:])

		if line[codeDigitCount] != '-' {
			response.Code, err = strconv.Atoi(line[:codeDigitCount])
			if err != nil {
				return nil, errors.New("can't convert response code to int")
			}
			break
		}
	}

	codeClass := response.Code / 100
	if codeClass != 2 && codeClass != 3 {
		return nil, fmt.Errorf("server returned an error: %s", line)
	}
	return response, nil
}

func (client *Client) execute(command Command) (*Response, error) {
	line := strings.Join(command, " ")
	if client.Logger != nil {
		client.Logger.Printf("C: %s\n", line)
	}

	err := utils.WriteLine(client.stream, line)
	if err != nil {
		return nil, err
	}
	err = client.stream.Flush()
	if err != nil {
		return nil, err
	}

	return client.readResponse()
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

	resp, err := client.execute([]string{"EHLO", "localhost"})
	if err != nil {
		return err
	}
	tlsSupported := false
	for _, extension := range resp.Lines[1:] {
		if strings.EqualFold(extension, "STARTTLS") {
			tlsSupported = true
			break
		}
	}
	if tlsSupported {
		_, err = client.execute([]string{"STARTTLS"})
		if err != nil {
			return err
		}
		client.conn = tls.Client(client.conn, &tls.Config{ServerName: client.host})
		client.stream = newReadWriter(client.conn)

		resp, err = client.execute([]string{"EHLO", "localhost"})
		if err != nil {
			return err
		}
	}

	authPlainSupported := false
	for _, extension := range resp.Lines[1:] {
		tokens := strings.Fields(extension)
		if tokens[0] == "AUTH" {
			for _, mode := range tokens[1:] {
				if mode == "PLAIN" {
					authPlainSupported = true
					break
				}
			}
			break
		}
	}
	if !authPlainSupported {
		return errors.New("SMTP server doesn't support AUTH PLAIN even via TLS")
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
	authPlainMessage := base64.StdEncoding.EncodeToString([]byte("\x00" + login + "\x00" + password))
	_, err := client.execute([]string{"AUTH", "PLAIN", authPlainMessage})
	return err
}

func escapeBody(body []byte) []byte {
	s := utils.LineSep + string(body)
	return []byte(strings.Replace(s, utils.LineSep+".", utils.LineSep+"..", -1))
}

func (client *Client) SendMail(from string, to []string, message *message.Entity) error {
	_, err := client.execute([]string{"MAIL", "FROM:" + from})
	if err != nil {
		return err
	}

	for _, addr := range to {
		_, err = client.execute([]string{"RCPT", "TO:" + addr})
		if err != nil {
			return err
		}
	}

	_, err = client.execute([]string{"DATA"})
	if err != nil {
		return err
	}
	message.Body = escapeBody(message.Body)
	client.Logger.Println("[sending mail entity]")
	err = message.WriteTo(client.stream)
	if err != nil {
		return err
	}
	err = utils.WriteLine(client.stream, ".")
	if err != nil {
		return err
	}
	err = client.stream.Flush()
	if err != nil {
		return err
	}

	_, err = client.readResponse()
	return err
}
