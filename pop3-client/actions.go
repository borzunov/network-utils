package main

import (
	"./message"
	"./protocol"
	"./utils"
	"errors"
	"fmt"
	"github.com/olekukonko/tablewriter"
	"io/ioutil"
	"log"
	"mime"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
)

type ActionInfo struct {
	Handler         func(client *protocol.Client, args []string) error
	ArgDescriptions []string
}

var Actions = map[string]ActionInfo{
	"ls":   {HandleList, nil},
	"top":  {HandleTop, []string{"id", "count"}},
	"retr": {HandleRetr, []string{"id"}},

	"verbose": {HandleVerbose, nil},
}

func showListItem(client *protocol.Client, id int, table *tablewriter.Table) error {
	message, err := client.Top(id, 0)
	if err != nil {
		return err
	}

	date, err := time.Parse(time.RFC1123Z, message.Headers["Date"])
	if err != nil {
		return err
	}
	table.Append([]string{strconv.Itoa(id), date.Local().Format(time.RFC822),
		message.Headers["From"], message.Headers["Subject"], message.Headers["Content-Type"]})
	return nil
}

func showMessageText(message *message.Entity) error {
	date, err := time.Parse(time.RFC1123Z, message.Headers["Date"])
	if err != nil {
		return err
	}

	fmt.Printf("Date:    %s\n", date.Local().Format(time.RFC822))
	fmt.Printf("From:    %s\n", message.Headers["From"])
	fmt.Printf("To:      %s\n", message.Headers["To"])
	fmt.Printf("Subject: %s\n", message.Headers["Subject"])
	fmt.Println()

	textLines := findText(message)
	if textLines != nil {
		fmt.Println(strings.Join(textLines, "\n"))
	} else {
		fmt.Println("[*] No text downloaded")
	}
	return nil
}

func HandleList(client *protocol.Client, args []string) error {
	list, err := client.List()
	if err != nil {
		return err
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetBorders(tablewriter.Border{Left: true, Top: false, Right: true, Bottom: false})
	table.SetCenterSeparator("|")
	table.SetHeader([]string{"Id", "Date", "From", "Subject", "Content-Type"})

	for _, item := range list {
		err := showListItem(client, item.Id, table)
		if err != nil {
			log.Printf("Failed to show a list item: %s\n", err)
		}
	}
	table.Render()
	return nil
}

func traverseEntity(msg *message.Entity, f func(part *message.Entity) error) error {
	err := f(msg)
	if err != nil {
		return err
	}

	for _, part := range msg.Parts {
		err := traverseEntity(&part, f)
		if err != nil {
			return err
		}
	}
	return nil
}

var textTypes = []string{"text/plain", "text/html"}

func findText(msg *message.Entity) []string {
	for _, targetType := range textTypes {
		var foundBody []string
		traverseEntity(msg, func(part *message.Entity) error {
			contentType, ok := part.Headers["Content-Type"]
			if ok {
				mediaType, _, err := mime.ParseMediaType(contentType)
				if err == nil && mediaType == targetType && part.Body != nil {
					foundBody = part.Body
					return errors.New("found")
				}
			}
			return nil
		})
		if foundBody != nil {
			return foundBody
		}
	}
	return nil
}

func HandleTop(client *protocol.Client, args []string) error {
	id, err := strconv.Atoi(args[0])
	if err != nil {
		return errors.New("<id> must be an integer")
	}
	count, err := strconv.Atoi(args[1])
	if err != nil {
		return errors.New("<count> must be an integer")
	}

	message, err := client.Top(id, count)
	if err != nil {
		return err
	}

	return showMessageText(message)
}

func saveAttachments(msg *message.Entity) error {
	return traverseEntity(msg, func(part *message.Entity) error {
		value, ok := part.Headers["Content-Disposition"]
		if ok {
			mediaType, params, err := mime.ParseMediaType(value)
			if err == nil && mediaType == "attachment" {
				filename, ok := params["filename"]
				if ok {
					data := []byte(strings.Join(part.Body, utils.LineSep))
					path := path.Join(config.AttachmentsDir, filename)
					err := ioutil.WriteFile(path, data, 0644)
					if err != nil {
						return err
					}

					fmt.Printf("[+] Attachment \"%s\" saved\n", path)
				} else {
					log.Printf("[-] Attachment of type \"%s\" doesn't specify the filename\n",
						part.Headers["Content-Type"])
				}
			}
		}
		return nil
	})
}

func HandleRetr(client *protocol.Client, args []string) error {
	id, err := strconv.Atoi(args[0])
	if err != nil {
		return errors.New("<id> must be an integer")
	}

	message, err := client.Retr(id)
	if err != nil {
		return err
	}

	err = showMessageText(message)
	if err != nil {
		return err
	}

	return saveAttachments(message)
}

func HandleVerbose(client *protocol.Client, args []string) error {
	client.Logger = log.New(os.Stdout, "", 0)
	return nil
}
