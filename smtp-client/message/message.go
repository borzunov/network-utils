package message

import (
	"../utils"
	"fmt"
	"io"
	"mime"
)

type Entity struct {
	Headers map[string]string
	Body    []byte
}

func (entity *Entity) WriteTo(writer io.Writer) error {
	for key, value := range entity.Headers {
		err := utils.WriteLine(writer, fmt.Sprintf("%s: %s", key, mime.BEncoding.Encode("utf-8", value)))
		if err != nil {
			return err
		}
	}
	err := utils.WriteLine(writer, "")
	if err != nil {
		return err
	}

	_, err = writer.Write(entity.Body)
	if err != nil {
		return err
	}
	_, err = writer.Write([]byte(utils.LineSep))
	return err
}
