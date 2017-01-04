package main

import (
	"./message"
	"./protocol"
	"./utils"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"mime"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

var (
	config struct {
		Host            string
		Port            int
		Login, Password string
	}
	messageConfig struct {
		Subject, From string
		To            []string
		ContentFile   string
		AttachedFiles []string
	}

	executableDir         = filepath.Dir(os.Args[0])
	configFilename        = path.Join(executableDir, "config.json")
	messageConfigFilename = path.Join(executableDir, "message.json")
)

func loadData(filename string, v interface{}) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	return json.NewDecoder(f).Decode(v)
}

func divideToLines(src []byte, lineLen int) []byte {
	reader := bytes.NewReader(src)
	buf := bytes.NewBuffer(make([]byte, 0, len(src)+len(src)/lineLen+1))
	part := make([]byte, lineLen)
	for {
		n, err := reader.Read(part)
		if err != nil {
			break
		}
		buf.Write(part[:n])
		buf.Write([]byte(utils.LineSep))
	}
	result := buf.Bytes()
	return result[:len(result)-1]
}

const boundary = "."

func makeMessage() (*message.Entity, error) {
	partPaths := append([]string{messageConfig.ContentFile}, messageConfig.AttachedFiles...)
	encoding := base64.StdEncoding
	body := new(bytes.Buffer)
	for i, path := range partPaths {
		var headers map[string]string
		if i == 0 {
			headers = map[string]string{
				"Content-Type": "text/plain; charset=utf-8",
			}
		} else {
			base := filepath.Base(path)
			mimeType := mime.TypeByExtension(filepath.Ext(base))
			headers = map[string]string{
				"Content-Disposition": mime.FormatMediaType("attachment", map[string]string{
					"filename": base,
				}),
				"Content-Type": mimeType,
			}
		}
		headers["Content-Transfer-Encoding"] = "base64"

		content, err := ioutil.ReadFile(path)
		if err != nil {
			return nil, err
		}
		encodedContent := make([]byte, encoding.EncodedLen(len(content)))
		encoding.Encode(encodedContent, content)
		encodedContent = divideToLines(encodedContent, 76)

		utils.WriteLine(body, "--"+boundary)
		(&message.Entity{
			Headers: headers,
			Body:    encodedContent,
		}).WriteTo(body)
	}
	utils.WriteLine(body, "--"+boundary+"--")

	return &message.Entity{
		Headers: map[string]string{
			"From":         messageConfig.From,
			"To":           strings.Join(messageConfig.To, ", "),
			"Subject":      messageConfig.Subject,
			"MIME-Version": "1.0",
			"Message-Id":   fmt.Sprintf("<%d.%s>", rand.Int63(), messageConfig.From),
			"Date":         time.Now().Format(time.RFC1123Z),
			"Content-Type": `multipart/mixed; boundary="."`,
		},
		Body: body.Bytes(),
	}, nil
}

func sendMessage(message *message.Entity) error {
	client := &protocol.Client{Logger: log.New(os.Stdout, "", 0)}
	err := client.Connect(config.Host, config.Port)
	if err != nil {
		return err
	}
	defer client.Close()

	err = client.Login(config.Login, config.Password)
	if err != nil {
		return err
	}

	err = client.SendMail(messageConfig.From, messageConfig.To, message)
	if err != nil {
		return err
	}
	return nil
}

func main() {
	err := loadData(configFilename, &config)
	if err != nil {
		log.Fatalf("failed to load %s: %s\n", configFilename, err)
	}
	err = loadData(messageConfigFilename, &messageConfig)
	if err != nil {
		log.Fatalf("failed to load %s: %s\n", messageConfigFilename, err)
	}

	message, err := makeMessage()
	if err != nil {
		log.Fatal(err)
	}
	err = sendMessage(message)
	if err != nil {
		log.Fatal(err)
	}
}
