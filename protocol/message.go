package protocol

import (
	"errors"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
)

type Header struct {
	Key, Value string
}

type MessageBase struct {
	Headers []Header
	Body    []byte
}

func (message *MessageBase) Header(key string) (string, bool) {
	found := false
	value := ""
	for _, header := range message.Headers {
		if strings.EqualFold(header.Key, key) {
			if found {
				value += ", "
			}
			found = true
			value += header.Value
		}
	}
	return value, found
}

func (message *MessageBase) SetHeader(key, value string) {
	for i, header := range message.Headers {
		if strings.EqualFold(header.Key, key) {
			header.Value = value

			message.Headers = append(message.Headers[:i+1],
				filterHeader(message.Headers[i+1:], key)...)
			return
		}
	}
	message.Headers = append(message.Headers, Header{key, value})
}

func filterHeader(headers []Header, key string) []Header {
	result := make([]Header, 0, len(headers))
	for _, header := range headers {
		if !strings.EqualFold(header.Key, key) {
			result = append(result, header)
		}
	}
	return result
}

func (message *MessageBase) DeleteHeader(key string) {
	message.Headers = filterHeader(message.Headers, key)
}

func (message *MessageBase) ReadFrom(conn *TextConn) error {
	for {
		line, err := conn.ReadLine()
		if err != nil {
			return err
		}
		if len(line) == 0 {
			break
		}

		parts := strings.Split(line, ": ")
		if len(parts) != 2 {
			return errors.New(fmt.Sprintf(`invalid header line "%s"`, line))
		}
		message.Headers = append(message.Headers, Header{parts[0], parts[1]})
	}

	if value, ok := message.Header("Transfer-Encoding"); ok {
		return errors.New(fmt.Sprintf(`Transfer-Encoding: %s is not supported`, value))
	}

	length := 0
	value, ok := message.Header("Content-Length")
	if ok {
		var err error
		length, err = strconv.Atoi(value)
		if err != nil {
			return errors.New("can't convert Content-Length to integer: " + err.Error())
		}
	}

	message.Body = make([]byte, length)
	return conn.ReadAll(message.Body)
}

func (message *MessageBase) WriteTo(conn *TextConn) error {
	// TODO: Deal with Transfer-Encoding
	message.SetHeader("Content-Length", strconv.Itoa(len(message.Body)))

	for _, header := range message.Headers {
		err := conn.WriteLine(header.Key + ": " + header.Value)
		if err != nil {
			return err
		}
	}
	err := conn.WriteLine("")
	if err != nil {
		return err
	}

	return conn.WriteAll(message.Body)
}

func logMessage(conn *TextConn, write bool, startLine string) {
	arrow := "<-"
	if write {
		arrow = "->"
	}
	log.Printf("%s %s  %s\n", arrow, conn.RemoteAddr(), startLine)
}

type Request struct {
	Method, Url, Protocol string
	MessageBase
}

var requestLineExp = regexp.MustCompile(`^(\w+) (.+) (HTTP/\S+)$`)

func (request *Request) ReadFrom(conn *TextConn) error {
	line, err := conn.ReadLine()
	if err != nil {
		return err
	}
	logMessage(conn, false, line)
	match := requestLineExp.FindStringSubmatch(line)
	if match == nil {
		return errors.New(fmt.Sprintf(`invalid request line "%s"`, line))
	}
	request.Method = match[1]
	request.Url = match[2]
	request.Protocol = match[3]

	return request.MessageBase.ReadFrom(conn)
}

func (request *Request) WriteTo(conn *TextConn) error {
	line := fmt.Sprintf("%s %s %s", request.Method, request.Url, request.Protocol)
	logMessage(conn, true, line)
	err := conn.WriteLine(line)
	if err != nil {
		return err
	}

	return request.MessageBase.WriteTo(conn)
}

type Response struct {
	Protocol string
	Code     int
	Reason   string
	MessageBase
}

var statusLineExp = regexp.MustCompile(`^(HTTP/\S+) (\d{3}) (.+)$`)

func (response *Response) ReadFrom(conn *TextConn) error {
	line, err := conn.ReadLine()
	logMessage(conn, false, line)
	if err != nil {
		return err
	}
	match := statusLineExp.FindStringSubmatch(line)
	if match == nil {
		return errors.New(fmt.Sprintf(`invalid status line "%s"`, line))
	}
	response.Protocol = match[1]
	response.Code, _ = strconv.Atoi(match[2])
	response.Reason = match[3]

	return response.MessageBase.ReadFrom(conn)
}

func (response *Response) WriteTo(conn *TextConn) error {
	line := fmt.Sprintf("%s %d %s", response.Protocol, response.Code, response.Reason)
	logMessage(conn, true, line)
	err := conn.WriteLine(line)
	if err != nil {
		return err
	}

	return response.MessageBase.WriteTo(conn)
}
