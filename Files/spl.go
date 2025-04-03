package main

import (
        "bytes"
        "encoding/json"
        "fmt"
        "io/ioutil"
        "net/http"
        "net/url"
        "os"
)

type SplunkResults struct {
        Results []map[string]interface{} `json:"results"`
}

func main() {
        splunkURL := os.Getenv("SPLUNK_URL") // e.g., "https://your-splunk-instance:8089"
        splunkUsername := os.Getenv("SPLUNK_USERNAME")
        splunkPassword := os.Getenv("SPLUNK_PASSWORD")
        searchQuery := "search index=your_index | head 10 | fields _raw" // Your Splunk search query.

        results, err := fetchSplunkLogs(splunkURL, splunkUsername, splunkPassword, searchQuery)
        if err != nil {
                fmt.Println("Error fetching Splunk logs:", err)
                return
        }

        // Process the Splunk results
        for _, result := range results {
                // Access individual fields in the result map
                rawData, ok := result["_raw"].(string) //accessing _raw field.
                if ok {
                        fmt.Println(rawData)
                }

                jsonData, err := json.Marshal(result)
                if err != nil {
                        fmt.Println("Error marshalling to JSON:", err)
                        continue
                }

                fmt.Println(string(jsonData)) //Print entire result as JSON.
        }
}

func fetchSplunkLogs(splunkURL, username, password, searchQuery string) ([]map[string]interface{}, error) {
        loginURL := splunkURL + "/services/auth/login"
        searchURL := splunkURL + "/services/search/jobs/export"

        // 1. Login to Splunk and get the session key
        loginData := url.Values{}
        loginData.Set("username", username)
        loginData.Set("password", password)

        loginResp, err := http.PostForm(loginURL, loginData)
        if err != nil {
                return nil, fmt.Errorf("login failed: %v", err)
        }
        defer loginResp.Body.Close()

        loginBody, err := ioutil.ReadAll(loginResp.Body)
        if err != nil {
                return nil, fmt.Errorf("failed to read login response: %v", err)
        }

        var loginResult map[string]interface{}
        err = json.Unmarshal(loginBody, &loginResult)
        if err != nil {
                return nil, fmt.Errorf("failed to parse login response: %v", err)
        }

        sessionKey, ok := loginResult["sessionKey"].(string)
        if !ok {
                return nil, fmt.Errorf("session key not found in login response")
        }

        // 2. Execute the search query
        searchData := url.Values{}
        searchData.Set("search", searchQuery)
        searchData.Set("output_mode", "json")
        searchData.Set("count", "1000") //Set count to large number if needed.

        req, err := http.NewRequest("POST", searchURL, bytes.NewBufferString(searchData.Encode()))
        if err != nil {
                return nil, fmt.Errorf("failed to create search request: %v", err)
        }

        req.Header.Set("Authorization", "Splunk "+sessionKey)
        req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

        client := &http.Client{}
        searchResp, err := client.Do(req)
        if err != nil {
                return nil, fmt.Errorf("search failed: %v", err)
        }
        defer searchResp.Body.Close()

        searchBody, err := ioutil.ReadAll(searchResp.Body)
        if err != nil {
                return nil, fmt.Errorf("failed to read search response: %v", err)
        }

        var searchResults SplunkResults
        err = json.Unmarshal(searchBody, &searchResults)
        if err != nil {
                return nil, fmt.Errorf("failed to parse search response: %v", err)
        }

        return searchResults.Results, nil
}



package main

import (
        "bufio"
        "bytes"
        "encoding/json"
        "fmt"
        "io"
        "net/http"
        "net/url"
        "os"
        "strings"
)

func main() {
        splunkURL := os.Getenv("SPLUNK_URL")
        splunkUsername := os.Getenv("SPLUNK_USERNAME")
        splunkPassword := os.Getenv("SPLUNK_PASSWORD")
        searchQuery := "search index=your_index | head 10 | fields _raw"

        err := fetchAndProcessSplunkLogs(splunkURL, splunkUsername, splunkPassword, searchQuery)
        if err != nil {
                fmt.Println("Error:", err)
        }
}

func fetchAndProcessSplunkLogs(splunkURL, username, password, searchQuery string) error {
        loginURL := splunkURL + "/services/auth/login"
        searchURL := splunkURL + "/services/search/jobs/export"

        // 1. Login
        loginData := url.Values{}
        loginData.Set("username", username)
        loginData.Set("password", password)

        loginResp, err := http.PostForm(loginURL, loginData)
        if err != nil {
                return fmt.Errorf("login failed: %v", err)
        }
        defer loginResp.Body.Close()

        var loginResult map[string]interface{}
        err = json.NewDecoder(loginResp.Body).Decode(&loginResult)
        if err != nil {
                return fmt.Errorf("failed to parse login response: %v", err)
        }

        sessionKey, ok := loginResult["sessionKey"].(string)
        if !ok {
                return fmt.Errorf("session key not found")
        }

        // 2. Search
        searchData := url.Values{}
        searchData.Set("search", searchQuery)
        searchData.Set("output_mode", "json")
        searchData.Set("count", "0")

        req, err := http.NewRequest("POST", searchURL, bytes.NewBufferString(searchData.Encode()))
        if err != nil {
                return fmt.Errorf("failed to create search request: %v", err)
        }

        req.Header.Set("Authorization", "Splunk "+sessionKey)
        req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

        client := &http.Client{}
        searchResp, err := client.Do(req)
        if err != nil {
                return fmt.Errorf("search failed: %v", err)
        }
        defer searchResp.Body.Close()

        // 3. Process the JSON stream
        reader := bufio.NewReader(searchResp.Body)
        for {
                line, err := reader.ReadString('\n')
                if err != nil {
                        if err == io.EOF {
                                break
                        }
                        return fmt.Errorf("error reading line: %v", err)
                }

                line = strings.TrimSpace(line)
                if line == "" {
                        continue // Skip empty lines.
                }

                var result map[string]interface{}
                err = json.Unmarshal([]byte(line), &result)
                if err != nil {
                        fmt.Printf("Error unmarshaling line: %v, line: %s\n", err, line)
                        continue // Skip bad lines.
                }

                // Process the result map
                fmt.Println(result)
                if raw, ok := result["_raw"].(string); ok {
                        fmt.Println("_raw:", raw)
                }
        }
        return nil
}

