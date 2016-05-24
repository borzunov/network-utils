package protocol

import (
	"bytes"
	"errors"
	"net"
)

type TextConn struct {
	net.Conn
	readBuf []byte
}

var lineSep = []byte("\r\n")

func copyBytes(slice []byte) []byte {
	result := make([]byte, len(slice))
	copy(result, slice)
	return result
}

func (conn *TextConn) ReadLineBytes() ([]byte, error) {
	indexOffset := 0
	buf := make([]byte, 1024)
	for {
		index := bytes.Index(conn.readBuf[indexOffset:], lineSep)
		if index != -1 {
			line := copyBytes(conn.readBuf[:indexOffset + index])
			conn.readBuf = conn.readBuf[indexOffset + index + len(lineSep):]
			return line, nil
		}

		n, err := conn.Read(buf)
		if err != nil {
			return nil, err
		}
		if n == 0 {
			return nil, errors.New("unexpected end of stream")
		}
		indexOffset = len(conn.readBuf) - len(lineSep) + 1
		if indexOffset < 0 {
			indexOffset = 0
		}
		conn.readBuf = append(conn.readBuf, buf[:n]...)
	}
}

func (conn *TextConn) Read(data []byte) (int, error) {
	readFromBuf := copy(data, conn.readBuf)
	conn.readBuf = conn.readBuf[readFromBuf:]

	readFromNet := 0
	if readFromBuf < len(data) {
		var err error
		readFromNet, err = conn.Conn.Read(data[readFromBuf:])
		if err != nil {
			return 0, err
		}
	}
	return readFromBuf + readFromNet, nil
}

func (conn *TextConn) processAll(method func(data []byte)(int, error), data []byte) error {
	for len(data) > 0 {
		n, err := method(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return errors.New("connection closed")
		}
		data = data[n:]
	}
	return nil
}

func (conn *TextConn) ReadAll(data []byte) error {
	return conn.processAll(conn.Read, data)
}

func (conn *TextConn) WriteAll(data []byte) error {
	return conn.processAll(conn.Write, data)
}

func (conn *TextConn) WriteLineBytes(line []byte) error {
	return conn.WriteAll(append(line, lineSep...))
}

func (conn *TextConn) ReadLine() (string, error) {
	result, err := conn.ReadLineBytes()
	if err != nil {
		return "", err
	}
	return string(result), nil
}

func (conn *TextConn) WriteLine(line string) error {
	return conn.WriteLineBytes([]byte(line))
}
