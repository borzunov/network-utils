package protocol

import (
	"compress/gzip"
	"compress/zlib"
	"io"
	"io/ioutil"
)

const (
	EncodingGzip = "gzip"
	EncodingZlib = "zlib"
)

func (message *MessageBase) DecodedBodyReader() (io.ReadCloser, error) {
	value, ok := message.Header("Content-Encoding")
	if ok {
		switch value {
		case EncodingGzip:
			return gzip.NewReader(message.Body.Reader)
		case EncodingZlib:
			return zlib.NewReader(message.Body.Reader)
		}
	}
	return ioutil.NopCloser(message.Body.Reader), nil
}

type nopWriteCloser struct {
	io.Writer
}

func (writer nopWriteCloser) Close() error {
	return nil
}

func (message *MessageBase) DecodedBodyWriter() io.WriteCloser {
	value, ok := message.Header("Content-Encoding")
	if ok {
		switch value {
		case EncodingGzip:
			return gzip.NewWriter(message.Body.Writer)
		case EncodingZlib:
			return zlib.NewWriter(message.Body.Writer)
		}
	}
	return nopWriteCloser{message.Body.Writer}
}
