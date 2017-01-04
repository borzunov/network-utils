package main

import (
	"./protocol"
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
)

var (
	config struct {
		Host            string
		Port            int
		Login, Password string

		AttachmentsDir string
	}

	executableDir  = filepath.Dir(os.Args[0])
	configFilename = path.Join(executableDir, "config.json")
)

func loadData(filename string, v interface{}) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	return json.NewDecoder(f).Decode(v)
}

func runPrompt() error {
	client := new(protocol.Client)
	err := client.Connect(config.Host, config.Port)
	if err != nil {
		return err
	}
	defer client.Close()

	err = client.Login(config.Login, config.Password)
	if err != nil {
		return err
	}

	fmt.Println("[+] Connected and logged in")
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print(">> ")
		if !scanner.Scan() {
			break
		}

		command := scanner.Text()
		tokens := strings.Split(command, " ")
		if len(tokens) == 0 {
			continue
		}
		action := strings.ToLower(tokens[0])
		if action == "exit" {
			break
		}

		info, ok := Actions[action]
		if !ok {
			fmt.Fprintf(os.Stderr, "[-] Unknown action \"%s\"\n", action)
			continue
		}
		args := tokens[1:]
		if len(args) != len(info.ArgDescriptions) {
			usage := []string{"[-] Usage: " + action}
			for _, desc := range info.ArgDescriptions {
				usage = append(usage, "<"+desc+">")
			}
			fmt.Fprintln(os.Stderr, strings.Join(usage, " "))
		}

		err := info.Handler(client, args)
		if err != nil {
			log.Println(err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to read stdin: %s", err)
	}
	return nil
}

func main() {
	err := loadData(configFilename, &config)
	if err != nil {
		log.Fatalf("failed to load %s: %s\n", configFilename, err)
	}

	err = runPrompt()
	if err != nil {
		log.Fatal(err)
	}
}
