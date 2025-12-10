package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/browserutils/kooky"
	_ "github.com/browserutils/kooky/browser/all"
	"github.com/gorilla/websocket"
	"golang.org/x/net/html"
)

var verbose bool

func debugPrintln(v ...interface{}) {
	if verbose {
		log.Println(v...)
	}
}

func debugPrintf(format string, v ...interface{}) {
	if verbose {
		log.Printf(format, v...)
	}
}

// Data Structures
type SubscriptionMessage struct {
	Command    string `json:"command"`
	Identifier string `json:"identifier"`
}

type PingMessage struct {
	Command    string `json:"command"`
	Identifier string `json:"identifier"`
	Data       string `json:"data"`
}

type IncomingMessage struct {
	Identifier string  `json:"identifier"`
	Message    Payload `json:"message"`
	Type       string  `json:"type"`
}

type Payload struct {
	Action  string   `json:"action"`
	Unreads []Unread `json:"unreads"`
}

type Unread struct {
	Section    string `json:"section"`
	Readable   string `json:"readable"`
	Identifier string `json:"identifier"`
}

type OutputFormat struct {
	Text  string `json:"text"`
	Class string `json:"class"`
}

// getCookieJar looks for a cookie store matching the provided profile name
func getCookieJar(profileName string) http.CookieJar {
	targetDomain := ".3.basecamp.com"
	ctx := context.Background()
	stores := kooky.FindAllCookieStores(ctx)

	var selectedStore kooky.CookieStore

	debugPrintf("Searching for cookies in profile containing: '%s'", profileName)

	for _, store := range stores {
		path := store.FilePath()

		if !strings.Contains(path, profileName) {
			continue
		}
		if !strings.HasSuffix(path, "Cookies") {
			continue
		}
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		}

		debugPrintf("Found valid cookie DB: %s", path)
		selectedStore = store
		break
	}

	if selectedStore == nil {
		debugPrintln("Exact profile match not found. Trying fallbacks...")
		for _, store := range stores {
			path := store.FilePath()
			if !strings.HasSuffix(path, "Cookies") {
				continue
			}
			if _, err := os.Stat(path); err == nil {
				selectedStore = store
				debugPrintf("Fallback: Using store at: %s", path)
				break
			}
		}
		if selectedStore == nil {
			log.Fatal("Fatal: No valid 'Cookies' file found on this system.")
		}
	}

	jar, err := selectedStore.SubJar(ctx, kooky.Domain(targetDomain))
	if err != nil {
		log.Fatalf("Error reading cookies from %s: %v", selectedStore.FilePath(), err)
	}

	return jar
}

func printOutput(value bool) {
	var output OutputFormat
	if value {
		output = OutputFormat{Text: " \ue800 ", Class: "has notifications"}
	} else {
		output = OutputFormat{Text: "", Class: "no notifications"}
	}
	outBytes, _ := json.Marshal(output)
	fmt.Println(string(outBytes))
}

func checkForReadings(htmlContent string) bool {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		log.Printf("Error parsing HTML: %v", err)
		return false
	}

	var foundNode *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if foundNode != nil {
			return
		}
		if n.Type == html.ElementNode {
			for _, attr := range n.Attr {
				if attr.Key == "data-readings" {
					foundNode = n
					return
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	if foundNode != nil {
		// Check for actual content (ignoring whitespace)
		for c := foundNode.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode {
				return true
			}
			if c.Type == html.TextNode && len(strings.TrimSpace(c.Data)) > 0 {
				return true
			}
		}
	}
	return false
}

func checkForUpdates(accountID string, profileName string, urlPath string) bool {
	targetURL := "https://3.basecamp.com/" + accountID + urlPath
	debugPrintf("Checking initial status via HTTP: %s", targetURL)

	client := &http.Client{
		Jar:     getCookieJar(profileName),
		Timeout: 10 * time.Second,
	}

	req, _ := http.NewRequest("GET", targetURL, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := client.Do(req)
	if err != nil {
		debugPrintf("Error fetching URL: %v", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return false
		}
		return checkForReadings(string(bodyBytes))
	}
	return false
}

func initialCheck(accountID string, profileName string) bool {
	debugPrintln("Performing initial check...")
	hasNotifications := checkForUpdates(accountID, profileName, "/my/navigation/readings")

	if !hasNotifications {
		hasNotifications = checkForUpdates(accountID, profileName, "/my/navigation/pings")
	}

	// Print the initial state immediately so the bar is populated
	printOutput(hasNotifications)

	return hasNotifications
}

func main() {
	flag.BoolVar(&verbose, "v", false, "Enable verbose logging")
	flag.Parse()

	accountID := os.Getenv("BASECAMP_NOTIFIER_ACCOUNT_ID")
	if accountID == "" {
		log.Fatal("Error: BASECAMP_NOTIFIER_ACCOUNT_ID required.")
	}

	profileName := os.Getenv("BASECAMP_NOTIFIER_CHROME_PROFILE")
	if profileName == "" {
		profileName = "Default"
	}

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	initialState := initialCheck(accountID, profileName)

	debugPrintf("Starting Basecamp Notifier for Account: %s (Profile: %s)", accountID, profileName)

	for {
		shouldExit := runConnection(interrupt, accountID, profileName, initialState)
		if shouldExit {
			break
		}

		debugPrintln("Connection lost. Reconnecting in 5 seconds...")

		// Note: On reconnection, we probably assume false initially, or you could re-run initialCheck() here.
		// For safety, we reset 'initialState' to false for subsequent re-connections to avoid stale state logic.
		initialState = false

		select {
		case <-time.After(5 * time.Second):
			continue
		case <-interrupt:
			return
		}
	}
}

func runConnection(interrupt <-chan os.Signal, accountID, profileName string, hasInitialNotifications bool) bool {
	u := url.URL{
		Scheme: "wss",
		Host:   "chat.3.basecamp.com",
		Path:   fmt.Sprintf("/%s", accountID),
	}
	debugPrintf("Connecting to %s", u.String())

	dialer := websocket.Dialer{
		Jar:              getCookieJar(profileName),
		HandshakeTimeout: 10 * time.Second,
	}

	requestHeaders := http.Header{}
	requestHeaders.Add("Origin", "https://chat.3.basecamp.com")

	c, _, err := dialer.Dial(u.String(), requestHeaders)
	if err != nil {
		debugPrintln("dial error:", err)
		return false
	}
	defer c.Close()

	var writeMutex sync.Mutex
	writeJSON := func(v interface{}) error {
		writeMutex.Lock()
		defer writeMutex.Unlock()
		return c.WriteJSON(v)
	}

	subUnreads := SubscriptionMessage{Command: "subscribe", Identifier: `{"channel":"UnreadsChannel"}`}
	if err := writeJSON(subUnreads); err != nil {
		debugPrintln("subscribe unreads error:", err)
		return false
	}

	subMonitor := SubscriptionMessage{Command: "subscribe", Identifier: `{"channel":"MonitoringChannel"}`}
	if err := writeJSON(subMonitor); err != nil {
		debugPrintln("subscribe monitor error:", err)
		return false
	}

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	errChan := make(chan error, 1)

	go func() {
		pingMsg := PingMessage{Command: "message", Identifier: `{"channel":"MonitoringChannel"}`, Data: `{"action":"ping"}`}
		for {
			select {
			case <-ticker.C:
				if err := writeJSON(pingMsg); err != nil {
					debugPrintln("ping write error:", err)
					errChan <- err
					return
				}
			case <-errChan:
				return
			}
		}
	}()

	ignoreInitialEmpty := hasInitialNotifications

	go func() {
		for {
			_, message, err := c.ReadMessage()
			if err != nil {
				debugPrintln("read error:", err)
				errChan <- err
				return
			}

			var incoming IncomingMessage
			if err := json.Unmarshal(message, &incoming); err != nil {
				continue
			}

			if incoming.Identifier == `{"channel":"UnreadsChannel"}` {
				unreadCount := len(incoming.Message.Unreads)

				if ignoreInitialEmpty {
					if unreadCount == 0 {
						debugPrintln("Ignoring initial empty socket message to preserve HTTP state.")
						// We flip the switch so subsequent empty messages ARE respected
						ignoreInitialEmpty = false
						continue
					} else {
						ignoreInitialEmpty = false
					}
				}

				printOutput(unreadCount > 0)
			}
		}
	}()

	select {
	case <-interrupt:
		debugPrintln("Interrupt received, closing...")
		writeMutex.Lock()
		c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		writeMutex.Unlock()
		return true
	case <-errChan:
		debugPrintln("Socket error detected, resetting connection...")
		return false
	}
}
