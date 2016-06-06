package main

import (
	"./protocol"
	"bytes"
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"io/ioutil"
	"strings"
)

func modifyContent(content []byte, selectors []string) []byte {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(content))
	if err != nil {
		return content
	}

	ads := doc.Find(strings.Join(selectors, ", "))
	ads.ReplaceWithHtml("<!-- An advertisment here was removed -->")
	doc.AppendHtml(fmt.Sprintf("<!-- This page was reassembled by %s. %d advertisment elements were removed. -->",
		ServerName, ads.Length()))

	html, err := doc.Html()
	if err != nil {
		return content
	}
	return []byte(html)
}

func ModifyResponse(url string, response *protocol.Response) error {
	var selectors []string
	for _, rule := range urlRules {
		if rule.Pattern.MatchString(url) {
			selectors = append(selectors, rule.Selectors...)
		}
	}
	if selectors == nil {
		return nil
	}

	value, ok := response.Header("Content-Type")
	if !ok {
		return nil
	}
	parts := strings.Split(value, "; ")
	if len(parts) == 0 || parts[0] != "text/html" {
		return nil
	}

	reader, err := response.DecodedBodyReader()
	if err != nil {
		return err
	}
	defer reader.Close()

	content, err := ioutil.ReadAll(reader)
	if err != nil {
		return err
	}

	content = modifyContent(content, selectors)

	response.SetChunked(false)
	response.Body = protocol.NewPipe()
	go func() {
		writer := response.DecodedBodyWriter()
		_, err := writer.Write(content)
		if err != nil {
			response.Body.Writer.CloseWithError(err)
		}
		response.Body.Writer.CloseWithError(writer.Close())
	}()
	return nil
}
