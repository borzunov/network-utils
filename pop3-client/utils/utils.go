package utils

import (
	"bufio"
	"errors"
	"io"
)

func ReadLine(reader *bufio.Reader) (string, error) {
	line, isPrefix, err := reader.ReadLine()
	if isPrefix && err != nil {
		err = errors.New("line is too long")
	}
	return string(line), err
}

const LineSep = "\r\n"

func WriteLine(writer io.Writer, line string) error {
	_, err := io.WriteString(writer, line+LineSep)
	return err
}
