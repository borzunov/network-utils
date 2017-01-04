package protocol

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"regexp"
	"strconv"
	"strings"
)

type Header struct {
	Key, Value string
}

type Pipe struct {
	Reader *io.PipeReader
	Writer *io.PipeWriter
}

func NewPipe() *Pipe {
	r, w := io.Pipe()
	return &Pipe{r, w}
}

type MessageBase struct {
	Headers []Header
	Body    *Pipe
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
			message.Headers[i].Value = value

			message.Headers = append(message.Headers[:i+1],
				filterHeader(message.Headers[i+1:], key)...)
			return
		}
	}
	message.Headers = append(message.Headers, Header{key, value})
}

func (message *MessageBase) Chunked() bool {
	value, ok := message.Header("Transfer-Encoding")
	return ok && strings.EqualFold(value, "chunked")
}

func (message *MessageBase) SetChunked(value bool) {
	if value {
		message.SetHeader("Transfer-Encoding", "chunked")
		message.DeleteHeader("Content-Length")
	} else {
		message.DeleteHeader("Transfer-Encoding")
		message.SetHeader("Content-Length", "0") // WriteTo will recalculate it
	}
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

func (message *MessageBase) readChunkFrom(reader io.Reader, length int) error {
	_, err := io.CopyN(message.Body.Writer, reader, int64(length))
	return err
}

func ReadLine(reader *bufio.Reader) (string, error) {
	line, isPrefix, err := reader.ReadLine()
	if isPrefix && err != nil {
		err = errors.New("line is too long")
	}
	return string(line), err
}

func WriteLine(writer io.Writer, line string) error {
	_, err := io.WriteString(writer, line+"\r\n")
	return err
}

func (message *MessageBase) readChunkedBodyFrom(reader *bufio.Reader) error {
	for {
		line, err := ReadLine(reader)
		if err != nil {
			return errors.New("failed to read chunk length: " + err.Error())
		}

		length, err := strconv.ParseUint(line, 16, 32)
		if err != nil {
			// TODO: Maybe implement chunk extension options
			return fmt.Errorf("can't parse chunk length %s", line)
		}
		if length == 0 {
			break
		}

		err = message.readChunkFrom(reader, int(length))
		if err != nil {
			return errors.New("failed to read chunk data: " + err.Error())
		}
		line, err = ReadLine(reader)
		if err != nil {
			return errors.New("failed to read CRLF after chunk data: " + err.Error())
		}
		if len(line) > 0 {
			return errors.New("chunk has more data than expected")
		}
	}
	for {
		line, err := ReadLine(reader)
		if err != nil {
			return errors.New("failed to read a trailer: " + err.Error())
		}
		if line == "" {
			break
		}
	}
	return nil
}

func (message *MessageBase) ReadFrom(reader *bufio.Reader) error {
	for {
		line, err := ReadLine(reader)
		if err != nil {
			return errors.New("failed to read a header: " + err.Error())
		}
		if line == "" {
			break
		}

		parts := strings.Split(line, ": ")
		if len(parts) != 2 {
			return errors.New(fmt.Sprintf(`invalid header line "%s"`, line))
		}
		message.Headers = append(message.Headers, Header{parts[0], parts[1]})
	}
	message.Body = NewPipe()

	if message.Chunked() {
		go func() { message.Body.Writer.CloseWithError(message.readChunkedBodyFrom(reader)) }()
		return nil
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
	go func() { message.Body.Writer.CloseWithError(message.readChunkFrom(reader, length)) }()
	return nil
}

func (message *MessageBase) setHopByHopHeaders() {
	if value, ok := message.Header("Connection"); ok {
		for _, item := range strings.Split(value, ", ") {
			if item != "close" {
				message.DeleteHeader(item)
			}
		}
	}
	message.SetHeader("Connection", "close")

	message.DeleteHeader("Upgrade")

	// FIXME: Maybe support Trailer
}

func (message *MessageBase) writeChunkedBodyTo(writer io.Writer) error {
	if message.Body != nil {
		for {
			buf := make([]byte, 4*1024*1024)
			n, err := message.Body.Reader.Read(buf)
			if n > 0 {
				err := WriteLine(writer, strconv.FormatUint(uint64(n), 16))
				if err != nil {
					return err
				}
				_, err = writer.Write(buf[:n])
				if err != nil {
					return err
				}
				err = WriteLine(writer, "")
				if err != nil {
					return err
				}
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
		}
	}
	err := WriteLine(writer, "0")
	if err != nil {
		return err
	}
	// FIXME: we may send here trailing headers
	return WriteLine(writer, "")
}

func (message *MessageBase) WriteTo(writer io.Writer) error {
	var body []byte
	if !message.Chunked() {
		if message.Body != nil {
			var err error
			body, err = ioutil.ReadAll(message.Body.Reader)
			if err != nil {
				return err
			}
		}
		if _, ok := message.Header("Content-Length"); ok {
			message.SetHeader("Content-Length", strconv.Itoa(len(body)))
		}
	}
	message.setHopByHopHeaders()

	for _, header := range message.Headers {
		err := WriteLine(writer, header.Key+": "+header.Value)
		if err != nil {
			return err
		}
	}
	err := WriteLine(writer, "")
	if err != nil {
		return err
	}

	if !message.Chunked() {
		_, err := writer.Write(body)
		return err
	}
	return message.writeChunkedBodyTo(writer)
}

func logMessage(conn net.Conn, write bool, startLine string) {
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

func (request *Request) ReadFrom(conn net.Conn) error {
	reader := bufio.NewReader(conn)

	line, err := ReadLine(reader)
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

	return request.MessageBase.ReadFrom(reader)
}

func (request *Request) WriteTo(conn net.Conn) error {
	writer := bufio.NewWriter(conn)

	line := fmt.Sprintf("%s %s %s", request.Method, request.Url, request.Protocol)
	logMessage(conn, true, line)
	err := WriteLine(writer, line)
	if err != nil {
		return err
	}

	err = request.MessageBase.WriteTo(writer)
	if err != nil {
		return err
	}
	return writer.Flush()
}

type Response struct {
	Protocol string
	Code     int
	Reason   string
	MessageBase
}

var statusLineExp = regexp.MustCompile(`^(HTTP/\S+) (\d{3}) (.+)$`)

func (response *Response) ReadFrom(conn net.Conn) error {
	reader := bufio.NewReader(conn)

	line, err := ReadLine(reader)
	if err != nil {
		return err
	}
	logMessage(conn, false, line)

	match := statusLineExp.FindStringSubmatch(line)
	if match == nil {
		return errors.New(fmt.Sprintf(`invalid status line "%s"`, line))
	}
	response.Protocol = match[1]
	response.Code, _ = strconv.Atoi(match[2])
	response.Reason = match[3]

	return response.MessageBase.ReadFrom(reader)
}

func (response *Response) WriteTo(conn net.Conn) error {
	writer := bufio.NewWriter(conn)

	line := fmt.Sprintf("%s %d %s", response.Protocol, response.Code, response.Reason)
	logMessage(conn, true, line)
	err := WriteLine(writer, line)
	if err != nil {
		return err
	}

	err = response.MessageBase.WriteTo(writer)
	if err != nil {
		return err
	}
	return writer.Flush()
}
