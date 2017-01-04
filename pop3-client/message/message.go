package message

import (
	"../utils"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"strings"
	"unicode"
)

type Entity struct {
	Headers map[string]string
	Body    []string
	Parts   []Entity
}

func decodeTransferEncoding(message *Entity) error {
	value, ok := message.Headers["Content-Transfer-Encoding"]
	if !ok {
		return nil
	}
	switch value {
	case "7bit", "8bit":
	case "base64":
		decoded, err := base64.StdEncoding.DecodeString(strings.Join(message.Body, ""))
		if err != nil {
			return err
		}
		message.Body = strings.Split(string(decoded), utils.LineSep)
	default:
		return fmt.Errorf(`"Content-Transfer-Encoding: %s" isn't supported`, value)
	}
	return nil
}

func decodeMultipartBody(message *Entity) error {
	contentType, ok := message.Headers["Content-Type"]
	if !ok {
		return nil
	}
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(mediaType, "multipart/") {
		return nil
	}
	boundary, ok := params["boundary"]
	if !ok {
		return fmt.Errorf("no boundary specified for content type %s", mediaType)
	}

	start := 0
	for i := 0; ; i++ {
		end := i == len(message.Body)
		if !end && !strings.HasPrefix(message.Body[i], "--"+boundary) {
			continue
		}

		part, err := FromLines(message.Body[start:i])
		if err != nil {
			return err
		}
		message.Parts = append(message.Parts, *part)

		if end || strings.HasPrefix(message.Body[i], "--"+boundary+"--") {
			break
		}
		start = i + 1
	}
	message.Body = nil // Store only inner parts without source lines
	return nil
}

func FromLines(lines []string) (*Entity, error) {
	entity := &Entity{Headers: make(map[string]string)}
	var i int
	var key string
	for i = 0; i < len(lines); i++ {
		line := lines[i]
		if line == "" {
			break
		}

		trimmed := strings.TrimSpace(line)
		if unicode.IsSpace(rune(line[0])) {
			if key == "" {
				return nil, errors.New("headers start with a continuation line")
			}
			entity.Headers[key] += trimmed
		} else {
			parts := strings.SplitN(trimmed, ": ", 2)
			key = parts[0]
			entity.Headers[key] = parts[1]
		}
	}
	decoder := new(mime.WordDecoder)
	decoder.CharsetReader = readCharset
	for key, value := range entity.Headers {
		decoded, err := decoder.DecodeHeader(value)
		if err != nil {
			return nil, fmt.Errorf(`can't decode a MIME header "%s: %s": %s`, key, value, err)
		}
		entity.Headers[key] = decoded
	}

	if i+1 < len(lines) {
		entity.Body = lines[i+1:]
		err := decodeTransferEncoding(entity)
		if err != nil {
			return nil, err
		}
		err = decodeMultipartBody(entity)
		if err != nil {
			return nil, err
		}
	}
	return entity, nil
}
