When running a Go application continuously for months with periodic requests, memory management becomes critical to prevent memory leaks, excessive allocation, and eventual crashes. Below,modified the previous Splunk client code to optimize memory usage for long-running scenarios and explain the strategies employed. Key areas of focus include minimizing allocations, reusing objects, handling garbage collection effectively, and cleaning up resources.
Here’s the updated code with memory management considerations:
```go
package main

import (
    "bufio"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "os"
    "path/filepath"
    "strings"
    "sync"
    "time"

    "golang.org/x/time/rate"
)

type SplunkConfig struct {
    BaseURL          string
    Username         string
    Password         string
    SearchQuery      string
    HTTPTimeout      time.Duration
    SessionFile      string
    MaxConcurrent    int
    RateLimitPerSec  int
    TokenRetryCount  int
    TokenRetryDelay  time.Duration
    SearchInterval   time.Duration // Interval between periodic searches
}

type SplunkSession struct {
    SessionKey string
    LastUsed   time.Time
    Timeout    time.Duration
    mu         sync.Mutex
}

type SplunkClient struct {
    Config      SplunkConfig
    Session     *SplunkSession
    Client      *http.Client
    Limiter     *rate.Limiter
    SearchChan  chan SearchRequest
    stopChan    chan struct{} // For graceful shutdown
    pool        sync.Pool     // Reuse buffers
    mu          sync.Mutex
    wg          sync.WaitGroup
}

type SearchRequest struct {
    Query    string
    ResultCh chan<- []map[string]interface{}
    ErrorCh  chan<- error
}

type LoginResponse struct {
    Session struct {
        Key     string `json:"key"`
        Timeout int    `json:"timeout"`
    } `json:"session"`
}

func NewSplunkClient(config SplunkConfig) *SplunkClient {
    client := &SplunkClient{
        Config:     config,
        Client:     &http.Client{Timeout: config.HTTPTimeout},
        Session:    &SplunkSession{},
        Limiter:    rate.NewLimiter(rate.Limit(config.RateLimitPerSec), config.RateLimitPerSec),
        SearchChan: make(chan SearchRequest, config.MaxConcurrent),
        stopChan:   make(chan struct{}),
        pool: sync.Pool{
            New: func() interface{} {
                return &bytes.Buffer{} // Reuse buffers for payloads
            },
        },
    }
    client.loadSession()
    go client.searchWorker()
    go client.periodicSearch() // Start periodic searches
    return client
}

func (sc *SplunkClient) loadSession() {
    sc.Session.mu.Lock()
    defer sc.Session.mu.Unlock()
    if _, err := os.Stat(sc.Config.SessionFile); os.IsNotExist(err) {
        return
    }
    data, err := os.ReadFile(sc.Config.SessionFile)
    if err != nil {
        fmt.Printf("Warning: Failed to load session: %v\n", err)
        return
    }
    if err := json.Unmarshal(data, sc.Session); err != nil {
        fmt.Printf("Warning: Failed to parse session: %v\n", err)
    }
}

func (sc *SplunkClient) saveSession() error {
    sc.Session.mu.Lock()
    defer sc.Session.mu.Unlock()
    data, err := json.Marshal(sc.Session)
    if err != nil {
        return fmt.Errorf("error marshaling session: %v", err)
    }
    return os.WriteFile(sc.Config.SessionFile, data, 0600)
}

func (sc *SplunkClient) Login() error {
    sc.Session.mu.Lock()
    defer sc.Session.mu.Unlock()

    url := fmt.Sprintf("%s/services/auth/login", sc.Config.BaseURL)
    buf := sc.pool.Get().(*bytes.Buffer)
    defer sc.pool.Put(buf)
    buf.Reset()
    fmt.Fprintf(buf, "username=%s&password=%s", sc.Config.Username, sc.Config.Password)

    for attempt := 1; attempt <= sc.Config.TokenRetryCount; attempt++ {
        req, err := http.NewRequest("POST", url, strings.NewReader(buf.String()))
        if err != nil {
            return fmt.Errorf("error creating login request: %v", err)
        }
        req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

        if err := sc.Limiter.Wait(context.Background()); err != nil {
            return fmt.Errorf("rate limit error: %v", err)
        }

        resp, err := sc.Client.Do(req)
        if err != nil {
            if attempt == sc.Config.TokenRetryCount {
                return fmt.Errorf("login failed after %d attempts: %v", attempt, err)
            }
            time.Sleep(sc.Config.TokenRetryDelay * time.Duration(attempt))
            continue
        }
        defer resp.Body.Close()

        if resp.StatusCode != http.StatusOK {
            body, _ := io.ReadAll(resp.Body)
            if attempt == sc.Config.TokenRetryCount {
                return fmt.Errorf("login failed after %d attempts with status %d: %s", attempt, resp.StatusCode, string(body))
            }
            time.Sleep(sc.Config.TokenRetryDelay * time.Duration(attempt))
            continue
        }

        var loginResp LoginResponse
        if err := json.NewDecoder(resp.Body).Decode(&loginResp); err != nil {
            return fmt.Errorf("error decoding login response: %v", err)
        }

        sc.Session.SessionKey = loginResp.Session.Key
        sc.Session.LastUsed = time.Now()
        sc.Session.Timeout = time.Duration(loginResp.Session.Timeout) * time.Second
        if err := sc.saveSession(); err != nil {
            fmt.Printf("Warning: Failed to save session: %v\n", err)
        }
        return nil
    }
    return nil
}

func (sc *SplunkClient) IsSessionValid() bool {
    sc.Session.mu.Lock()
    defer sc.Session.mu.Unlock()
    return sc.Session.SessionKey != "" && time.Since(sc.Session.LastUsed) < sc.Session.Timeout
}

func (sc *SplunkClient) searchWorker() {
    for {
        select {
        case req := <-sc.SearchChan:
            sc.wg.Add(1)
            go func(r SearchRequest) {
                defer sc.wg.Done()
                results, err := sc.fetchSplunkLogs(r.Query)
                if err != nil {
                    r.ErrorCh <- err
                    return
                }
                r.ResultCh <- results
            }(req)
        case <-sc.stopChan:
            return
        }
    }
}

func (sc *SplunkClient) FetchSplunkLogs(query string) ([]map[string]interface{}, error) {
    resultCh := make(chan []map[string]interface{}, 1) // Buffered to prevent goroutine leak
    errorCh := make(chan error, 1)
    sc.SearchChan <- SearchRequest{Query: query, ResultCh: resultCh, ErrorCh: errorCh}

    select {
    case results := <-resultCh:
        return results, nil
    case err := <-errorCh:
        return nil, err
    case <-time.After(sc.Config.HTTPTimeout): // Timeout for stuck requests
        return nil, fmt.Errorf("search timed out after %v", sc.Config.HTTPTimeout)
    }
}

func (sc *SplunkClient) fetchSplunkLogs(query string) ([]map[string]interface{}, error) {
    if !sc.IsSessionValid() {
        if err := sc.Login(); err != nil {
            return nil, fmt.Errorf("session refresh failed: %v", err)
        }
    }

    searchURL := fmt.Sprintf("%s/services/search/jobs", sc.Config.BaseURL)
    buf := sc.pool.Get().(*bytes.Buffer)
    defer sc.pool.Put(buf)
    buf.Reset()
    fmt.Fprintf(buf, "search=%s&output_mode=json&exec_mode=oneshot", query)

    req, err := http.NewRequest("POST", searchURL, strings.NewReader(buf.String()))
    if err != nil {
        return nil, fmt.Errorf("error creating search request: %v", err)
    }

    sc.Session.mu.Lock()
    req.Header.Set("Authorization", fmt.Sprintf("Splunk %s", sc.Session.SessionKey))
    sc.Session.LastUsed = time.Now()
    sc.Session.mu.Unlock()
    req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

    if err := sc.Limiter.Wait(context.Background()); err != nil {
        return nil, fmt.Errorf("rate limit error: %v", err)
    }

    resp, err := sc.Client.Do(req)
    if err != nil {
        return nil, fmt.Errorf("error executing search request: %v", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        body, _ := io.ReadAll(resp.Body)
        return nil, fmt.Errorf("search failed with status %d: %s", resp.StatusCode, string(body))
    }

    // Pre-allocate slice with reasonable capacity
    results := make([]map[string]interface{}, 0, 100) // Adjust capacity based on expected result size
    scanner := bufio.NewScanner(resp.Body)
    for scanner.Scan() {
        line := scanner.Text()
        if line == "" {
            continue
        }
        var result struct {
            Result map[string]interface{} `json:"result"`
        }
        if err := json.Unmarshal([]byte(line), &result); err != nil {
            var rawMap map[string]interface{}
            if err := json.Unmarshal([]byte(line), &rawMap); err != nil {
                continue // Skip invalid lines silently to avoid logging overhead
            }
            results = append(results, rawMap)
        } else if result.Result != nil {
            results = append(results, result.Result)
        }
    }
    if err := scanner.Err(); err != nil {
        return nil, fmt.Errorf("error reading search response: %v", err)
    }
    return results, nil
}

func (sc *SplunkClient) periodicSearch() {
    ticker := time.NewTicker(sc.Config.SearchInterval)
    defer ticker.Stop()
    for {
        select {
        case <-ticker.C:
            results, err := sc.FetchSplunkLogs(sc.Config.SearchQuery)
            if err != nil {
                fmt.Printf("Periodic search failed: %v\n", err)
                continue
            }
            // Process results minimally to avoid memory retention
            fmt.Printf("Periodic search found %d records\n", len(results))
            // Explicitly nil out results to help GC
            results = nil
        case <-sc.stopChan:
            return
        }
    }
}

func (sc *SplunkClient) Shutdown() {
    close(sc.stopChan)
    sc.wg.Wait()
    close(sc.SearchChan)
}

func main() {
    config := SplunkConfig{
        BaseURL:         "https://your-splunk-instance:8089",
        Username:        "your-username",
        Password:        "your-password",
        SearchQuery:     "search index=your_index sourcetype=your_sourcetype earliest=-15m latest=now",
        HTTPTimeout:     45 * time.Second,
        SessionFile:     "splunk_session.json",
        MaxConcurrent:   5,
        RateLimitPerSec: 10,
        TokenRetryCount: 3,
        TokenRetryDelay: 2 * time.Second,
        SearchInterval:  5 * time.Minute, // Periodic search every 5 minutes
    }

    client := NewSplunkClient(config)

    // Run indefinitely until interrupted
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
    <-sigChan

    fmt.Println("Shutting down...")
    client.Shutdown()
}
```
Memory Management Strategies:
Object Reuse with sync.Pool:
Added a sync.Pool for reusing *bytes.Buffer objects for request payloads.
Reduces allocations for repeated string building in Login() and fetchSplunkLogs().
Pre-allocated Slices:
In fetchSplunkLogs(), the results slice is pre-allocated with a capacity (e.g., 100) based on expected result size. Adjust this based on your typical Splunk response size to avoid frequent reallocations.
Buffered Channels:
resultCh and errorCh in FetchSplunkLogs() are buffered (capacity 1) to prevent goroutine leaks if the caller doesn’t consume the result immediately.
Explicit Nil Assignment:
In periodicSearch(), results is set to nil after processing to ensure the garbage collector (GC) can reclaim the memory, preventing retention of large result sets.
Graceful Shutdown:
Added Shutdown() method and stopChan to cleanly stop workers and periodic searches.
Uses sync.WaitGroup to wait for all goroutines to finish, preventing resource leaks on exit.
Periodic Search with Minimal Retention:
periodicSearch() runs on a configurable interval (SearchInterval) and processes results minimally.
Avoids storing large datasets long-term by printing only a count and releasing memory immediately.
Timeouts and Resource Cleanup:
Added a timeout in FetchSplunkLogs() to prevent hanging on stuck requests.
Ensures resp.Body.Close() is always called with defer to release network resources.
Reduced Logging Overhead:
Removed warning logs for individual JSON parsing failures to avoid excessive string allocations over months.
Critical errors are still logged with detailed context.
Garbage Collection Optimization:
Go’s GC is efficient by default, but we help it by:
Minimizing allocations with pooling and pre-allocation.
Ensuring large objects (e.g., results) are released promptly.
Avoiding unnecessary goroutine creation.
Signal Handling:
Added signal handling for graceful shutdown on SIGINT or SIGTERM, ensuring resources are cleaned up when the application stops.
Additional Considerations:
Memory Profiling:
Use Go’s pprof package to monitor memory usage over time:
```go
import "runtime/pprof"
// Add at startup:
f, _ := os.Create("mem.pprof")
pprof.WriteHeapProfile(f)
f.Close()
```
Run periodically or on demand to identify memory growth.
Adjust Capacities:
Tune MaxConcurrent, SearchChan buffer size, and results capacity based on your workload to balance memory use and performance.
Smaller Time Windows:
The example uses earliest=-15m instead of -24h to fetch smaller datasets, reducing memory pressure. Adjust based on your needs.
HTTP Client Reuse:
The single *http.Client is reused across all requests, avoiding repeated connection overhead and maintaining a connection pool.
Long-term Monitoring:
Add metrics (e.g., using expvar or a metrics library like Prometheus) to track memory usage, goroutine count, and request rates over time.
Running for Months:
The application now runs indefinitely with periodic searches every SearchInterval (e.g., 5 minutes).
Memory usage should stabilize because:
Objects are reused via sync.Pool.
Large allocations are pre-sized and released promptly.
Goroutines and channels are managed to prevent leaks.
Shutdown cleans up all resources.
Testing:
Simulate long-running behavior with a shorter SearchInterval (e.g., 10 seconds) and monitor memory usage with top or pprof.
Verify that memory doesn’t grow unbounded over hours or days.
This version should handle months of continuous operation with minimal memory growth, assuming Splunk responses remain consistent in size and frequency. Let me know if you need further tuning for your specific use case!



******************---------

Retrieving 2 million records from Splunk in a single API request poses significant challenges in terms of memory management, processing time, and system stability, especially in a long-running Go application. Loading all 2 million records into memory at once (e.g., as a slice of map[string]interface{}) could easily consume gigabytes of RAM, potentially crashing the application or degrading performance. To handle this efficiently, we need to:
Stream the Response: Process records incrementally instead of loading everything into memory.
Paginate Results: Use Splunk’s search job API to fetch records in manageable chunks.
Limit Memory Usage: Avoid large allocations by processing and discarding records as they’re read.
Optimize for Scale: Adjust buffer sizes and concurrency for high throughput.
Since the current code uses a oneshot search (exec_mode=oneshot), which returns all results immediately, it’s not ideal for 2 million records. Instead, we’ll switch to Splunk’s search job API, which allows creating a job, polling its status, and fetching results in batches using offset and count parameters. This approach streams results incrementally and keeps memory usage under control.
Updated Code for Handling 2 Million Records
Here’s the modified code using the search job API with streaming and pagination:
```go
package main

import (
    "bufio"
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "os"
    "path/filepath"
    "strings"
    "sync"
    "time"

    "golang.org/x/time/rate"
)

type SplunkConfig struct {
    BaseURL          string
    Username         string
    Password         string
    SearchQuery      string
    HTTPTimeout      time.Duration
    SessionFile      string
    MaxConcurrent    int
    RateLimitPerSec  int
    TokenRetryCount  int
    TokenRetryDelay  time.Duration
    SearchInterval   time.Duration
    InitialBufSize   int   // Initial buffer size for scanner
    MaxBufSize       int   // Maximum buffer size for scanner
    MaxResponseSize  int64 // Maximum allowed response size (bytes)
    BatchSize        int   // Number of records per fetch
}

type SplunkSession struct {
    SessionKey string
    LastUsed   time.Time
    Timeout    time.Duration
    mu         sync.Mutex
}

type SplunkClient struct {
    Config      SplunkConfig
    Session     *SplunkSession
    Client      *http.Client
    Limiter     *rate.Limiter
    SearchChan  chan SearchRequest
    stopChan    chan struct{}
    pool        sync.Pool
    mu          sync.Mutex
    wg          sync.WaitGroup
}

type SearchRequest struct {
    Query    string
    Process  func(map[string]interface{}) error // Callback to process each record
    ErrorCh  chan<- error
}

type LoginResponse struct {
    Session struct {
        Key     string `json:"key"`
        Timeout int    `json:"timeout"`
    } `json:"session"`
}

type SearchJobResponse struct {
    Sid string `json:"sid"`
}

type SearchResultsResponse struct {
    Results []map[string]interface{} `json:"results"`
}

func NewSplunkClient(config SplunkConfig) *SplunkClient {
    client := &SplunkClient{
        Config:     config,
        Client:     &http.Client{Timeout: config.HTTPTimeout},
        Session:    &SplunkSession{},
        Limiter:    rate.NewLimiter(rate.Limit(config.RateLimitPerSec), config.RateLimitPerSec),
        SearchChan: make(chan SearchRequest, config.MaxConcurrent),
        stopChan:   make(chan struct{}),
        pool: sync.Pool{
            New: func() interface{} {
                return &bytes.Buffer{}
            },
        },
    }
    client.loadSession()
    go client.searchWorker()
    go client.periodicSearch()
    return client
}

// Other functions (loadSession, saveSession, Login, IsSessionValid, Shutdown) remain unchanged

func (sc *SplunkClient) searchWorker() {
    for {
        select {
        case req := <-sc.SearchChan:
            sc.wg.Add(1)
            go func(r SearchRequest) {
                defer sc.wg.Done()
                err := sc.processSearchJob(r.Query, r.Process)
                if err != nil {
                    r.ErrorCh <- err
                }
            }(req)
        case <-sc.stopChan:
            return
        }
    }
}

func (sc *SplunkClient) FetchSplunkLogs(query string, process func(map[string]interface{}) error) error {
    errorCh := make(chan error, 1)
    sc.SearchChan <- SearchRequest{
        Query:   query,
        Process: process,
        ErrorCh: errorCh,
    }

    select {
    case err := <-errorCh:
        return err
    case <-time.After(sc.Config.HTTPTimeout):
        return fmt.Errorf("search timed out after %v", sc.Config.HTTPTimeout)
    }
}

func (sc *SplunkClient) createSearchJob(query string) (string, error) {
    if !sc.IsSessionValid() {
        if err := sc.Login(); err != nil {
            return "", fmt.Errorf("session refresh failed: %v", err)
        }
    }

    url := fmt.Sprintf("%s/services/search/jobs", sc.Config.BaseURL)
    buf := sc.pool.Get().(*bytes.Buffer)
    defer sc.pool.Put(buf)
    buf.Reset()
    fmt.Fprintf(buf, "search=%s&output_mode=json", query)

    req, err := http.NewRequest("POST", url, strings.NewReader(buf.String()))
    if err != nil {
        return "", fmt.Errorf("error creating search job request: %v", err)
    }

    sc.Session.mu.Lock()
    req.Header.Set("Authorization", fmt.Sprintf("Splunk %s", sc.Session.SessionKey))
    sc.Session.LastUsed = time.Now()
    sc.Session.mu.Unlock()
    req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

    if err := sc.Limiter.Wait(context.Background()); err != nil {
        return "", fmt.Errorf("rate limit error: %v", err)
    }

    resp, err := sc.Client.Do(req)
    if err != nil {
        return "", fmt.Errorf("error executing search job request: %v", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusCreated {
        body, _ := io.ReadAll(resp.Body)
        return "", fmt.Errorf("search job creation failed with status %d: %s", resp.StatusCode, string(body))
    }

    var jobResp SearchJobResponse
    if err := json.NewDecoder(resp.Body).Decode(&jobResp); err != nil {
        return "", fmt.Errorf("error decoding search job response: %v", err)
    }

    return jobResp.Sid, nil
}

func (sc *SplunkClient) processSearchJob(query string, process func(map[string]interface{}) error) error {
    // Create search job
    sid, err := sc.createSearchJob(query)
    if err != nil {
        return err
    }

    // Wait for job to complete (simplified; in production, poll status)
    time.Sleep(5 * time.Second) // Adjust based on query complexity

    // Fetch results in batches
    offset := 0
    batchSize := sc.Config.BatchSize
    if batchSize == 0 {
        batchSize = 10000 // Default batch size
    }

    for {
        url := fmt.Sprintf("%s/services/search/jobs/%s/results?output_mode=json&offset=%d&count=%d", sc.Config.BaseURL, sid, offset, batchSize)
        req, err := http.NewRequest("GET", url, nil)
        if err != nil {
            return fmt.Errorf("error creating results request: %v", err)
        }

        sc.Session.mu.Lock()
        req.Header.Set("Authorization", fmt.Sprintf("Splunk %s", sc.Session.SessionKey))
        sc.Session.LastUsed = time.Now()
        sc.Session.mu.Unlock()

        if err := sc.Limiter.Wait(context.Background()); err != nil {
            return fmt.Errorf("rate limit error: %v", err)
        }

        resp, err := sc.Client.Do(req)
        if err != nil {
            return fmt.Errorf("error fetching results: %v", err)
        }

        if resp.StatusCode != http.StatusOK {
            body, _ := io.ReadAll(resp.Body)
            resp.Body.Close()
            return fmt.Errorf("results fetch failed with status %d: %s", resp.StatusCode, string(body))
        }

        // Check Content-Length if available
        if resp.ContentLength > 0 && sc.Config.MaxResponseSize > 0 && resp.ContentLength > sc.Config.MaxResponseSize {
            resp.Body.Close()
            return fmt.Errorf("batch response size (%d bytes) exceeds max allowed size (%d bytes)", resp.ContentLength, sc.Config.MaxResponseSize)
        }

        decoder := json.NewDecoder(resp.Body)
        var results SearchResultsResponse
        if err := decoder.Decode(&results); err != nil {
            resp.Body.Close()
            return fmt.Errorf("error decoding results: %v", err)
        }

        // Process each record incrementally
        for _, record := range results.Results {
            if err := process(record); err != nil {
                resp.Body.Close()
                return fmt.Errorf("error processing record: %v", err)
            }
        }

        resp.Body.Close()

        // Check if we’ve processed all records
        if len(results.Results) < batchSize {
            break // No more results
        }

        offset += batchSize
    }

    return nil
}

func main() {
    config := SplunkConfig{
        BaseURL:          "https://your-splunk-instance:8089",
        Username:         "your-username",
        Password:         "your-password",
        SearchQuery:      "search index=your_index sourcetype=your_sourcetype earliest=-15m latest=now",
        HTTPTimeout:      45 * time.Second,
        SessionFile:      "splunk_session.json",
        MaxConcurrent:    5,
        RateLimitPerSec:  10,
        TokenRetryCount:  3,
        TokenRetryDelay:  2 * time.Second,
        SearchInterval:   5 * time.Minute,
        InitialBufSize:   128 * 1024,      // Start with 128KB
        MaxBufSize:       10 * 1024 * 1024, // Allow up to 10MB per line
        MaxResponseSize:  50 * 1024 * 1024, // Max response size: 50MB
        BatchSize:        10000,            // Fetch 10,000 records per batch
    }

    client := NewSplunkClient(config)

    // Example: Process 2 million records
    err := client.FetchSplunkLogs(config.SearchQuery, func(record map[string]interface{}) error {
        // Example processing: Print timestamp and raw message
        if ts, ok := record["_time"]; ok {
            fmt.Printf("Timestamp: %v\n", ts)
        }
        if raw, ok := record["_raw"]; ok {
            fmt.Printf("Raw: %v\n", raw)
        }
        return nil
    })
    if err != nil {
        fmt.Printf("Error processing search: %v\n", err)
    }

    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
    <-sigChan

    fmt.Println("Shutting down...")
    client.Shutdown()
}

```
# Key Changes and Explanations
New Config Field
BatchSize: Number of records to fetch per API call (default 10,000). Controls memory usage per batch.
Updated SearchRequest
Process: A callback function to handle each record as it’s received, avoiding storing all 2 million records in memory.
createSearchJob(query string) (string, error)
What it Does: Creates a Splunk search job and returns its SID (Search ID).
Key Actions:
Sends a POST request to /services/search/jobs with the query.
Parses the response to extract the sid.
Memory: Minimal; only stores the SID string.
processSearchJob(query string, process func(map[string]interface{}) error) error
What it Does: Manages the search job and fetches results in batches, processing each record incrementally.
Key Actions:
Creates a search job with createSearchJob.
Waits briefly (simplified; production code should poll /services/search/jobs/{sid} for status).
Fetches results in batches using offset and BatchSize:
GET request to /services/search/jobs/{sid}/results.
Checks Content-Length per batch to reject oversized responses.
Decodes JSON into a SearchResultsResponse struct with a fixed-size slice.
Calls process for each record, releasing memory after each batch.
Continues until fewer than BatchSize records are returned (indicating completion).
Memory Management:
Only holds BatchSize records in memory at a time (e.g., 10,000).
Closes resp.Body after each batch to free resources.
Avoids accumulating all 2 million records.
FetchSplunkLogs(query string, process func(map[string]interface{}) error) error
What it Does: Public API to submit a search request with a processing callback.
Key Actions:
Submits the request to SearchChan with the query and callback.
Waits for completion or timeout.
Memory: Minimal; channels are buffered to prevent leaks.
Memory Usage Estimate
Per Batch: Assuming 1KB per record (typical JSON log size), 10,000 records ≈ 10MB.
Total: With 2 million records, 200 batches of 10MB each, processed sequentially or concurrently up to MaxConcurrent.
Peak: At most MaxConcurrent * BatchSize * record_size (e.g., 5 * 10,000 * 1KB = 50MB).
Handling 2 Million Records
Streaming:
The process callback processes each record as it’s received, avoiding a giant results slice.
Example: Print fields, write to a file, or send to a database.
Pagination:
Uses Splunk’s offset and count parameters to fetch 10,000 records at a time.
Loops until all records are processed (200 iterations for 2 million).
Memory Control:
BatchSize limits memory per batch (e.g., 10MB for 10,000 records).
MaxResponseSize caps each batch’s size (e.g., 50MB).
MaxConcurrent limits concurrent batches (e.g., 5 * 10MB = 50MB peak).
Scalability:
Adjust BatchSize based on memory availability (e.g., 5,000 for lower memory systems).
Increase MaxConcurrent for faster processing if CPU/memory allows.
Tuning Recommendations
BatchSize:
Default: 10,000 (~10MB).
Lower (e.g., 5,000) for memory-constrained systems; higher (e.g., 20,000) for more RAM.
MaxResponseSize:
Set to 2-5x BatchSize * avg_record_size (e.g., 50MB for 10MB batches).
MaxConcurrent:
Set to 1-5 based on CPU cores and memory (e.g., 5 = 50MB peak).
Polling:
Replace time.Sleep with status polling:
go
for {
    statusResp, _ := sc.Client.Get(fmt.Sprintf("%s/services/search/jobs/%s?output_mode=json", sc.Config.BaseURL, sid))
    var status struct { IsDone bool `json:"isDone"` }
    json.NewDecoder(statusResp.Body).Decode(&status)
    statusResp.Body.Close()
    if status.IsDone { break }
    time.Sleep(1 * time.Second)
}
Processing:
Write to a file or database instead of printing:
go
file, _ := os.Create("splunk_logs.txt")
defer file.Close()
process := func(record map[string]interface{}) error {
    data, _ := json.Marshal(record)
    _, err := file.Write(append(data, '\n'))
    return err
}
Conclusion
This approach handles 2 million records by:
Creating a search job and fetching results in batches (e.g., 10,000 records).
Processing each record via a callback, keeping memory usage low (e.g., 10-50MB peak).
Leveraging Splunk’s pagination to avoid loading everything at once.
Test with your query to ensure the job completes and adjust BatchSize based on your system’s memory. Let me know if you need further optimization or a different streaming strategy!