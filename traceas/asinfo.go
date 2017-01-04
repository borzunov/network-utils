package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
)

type APIItem struct {
	Key, Value string
}

type APIResponse struct {
	Data struct {
		Records     [][]APIItem
		Irr_Records [][]APIItem
	}
}

func getAPIResponse(ip string) (*APIResponse, error) {
	resp, err := http.Get(fmt.Sprintf("https://stat.ripe.net/data/whois/data.json?resource=%s", ip))
	if err != nil {
		return nil, err
	}
	content, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}

	var result APIResponse
	if err := json.Unmarshal(content, &result); err != nil {
		return nil, errors.New("Invalid response format")
	}
	return &result, nil
}

type ASInfo struct {
	Number        int
	Country       string
	ProviderDescr []string
}

func getASInfo(ip string) (ASInfo, error) {
	response, err := getAPIResponse(ip)
	if err != nil {
		return ASInfo{}, err
	}
	var result ASInfo

	providerKnown := false
	for _, record := range response.Data.Records {
		directAllocation := true
		for _, item := range record {
			key := strings.ToLower(item.Key)
			if key == "nettype" && item.Value != "Direct Allocation" {
				directAllocation = false
				break
			}
		}
		if !directAllocation {
			// ARIN inserts own record in information about all its IPs. We skip this record to
			// find more specific organization name.
			continue
		}

		for _, item := range record {
			key := strings.ToLower(item.Key)
			if key == "country" && result.Country == "" {
				result.Country = item.Value
			} else if (key == "netname" || key == "descr" || key == "owner" || key == "organization") &&
				!providerKnown {
				result.ProviderDescr = append(result.ProviderDescr, item.Value)
			}
		}
		if len(result.ProviderDescr) > 0 {
			providerKnown = true
		}
	}

	for _, record := range response.Data.Irr_Records {
		for _, item := range record {
			key := strings.ToLower(item.Key)
			if (key == "origin" || key == "originas") && result.Number == 0 {
				result.Number, err = strconv.Atoi(strings.TrimPrefix(item.Value, "AS"))
				if err != nil {
					return ASInfo{}, errors.New("Invalid ASN")
				}
			}
		}
	}

	return result, nil
}
