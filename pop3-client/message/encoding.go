package message

import (
	"fmt"
	"golang.org/x/text/encoding/htmlindex"
	"io"
)

func readCharset(charset string, input io.Reader) (io.Reader, error) {
	encoding, err := htmlindex.Get(charset)
	if err != nil {
		return nil, fmt.Errorf(`unknown charset "%s"`, charset)
	}
	return encoding.NewDecoder().Reader(input), nil
}
