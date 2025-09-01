package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gtuk/discordwebhook"
	"github.com/joho/godotenv"
	"github.com/playwright-community/playwright-go"
	"github.com/yunexpress-order-checker/internal/validation"
)

type Proxies struct {
	Proxies []string
}

type Request struct {
	NumberList          []string `json:"NumberList"`
	CaptchaVerification string   `json:"CaptchaVerification"`
	Year                int      `json:"Year"`
	Timestamp           int64    `json:"Timestamp"`
	Signature           string   `json:"Signature"`
}

type Result struct {
	ID        string    `json:"Id"`
	Status    int       `json:"Status"`
	TrackInfo TrackInfo `json:"TrackInfo"`
}

type Results struct {
	Results []Result `json:"ResultList"`
}

type TrackInfo struct {
	WaybillNumber       string         `json:"WaybillNumber"`
	TrackingNumber      string         `json:"TrackingNumber"`
	CustomerOrderNumber string         `json:"CustomerOrderNumber"`
	Weight              float64        `json:"Weight"`
	LastTrackEvent      LastTrackEvent `json:"LastTrackEvent"`
	TrackEventDetails   []TrackEvent   `json:"TrackEvent"`
}

type LastTrackEvent struct {
	ProcessDate     string `json:"ProcessDate"`
	ProcessContent  string `json:"ProcessContent"`
	ProcessLocation string `json:"ProcessLocation"`
}

type TrackEvent struct {
	ProcessLocation string `json:"ProcessLocation"`
	CreatedOn       string `json:"CreatedOn"`
	ProcessContent  string `json:"ProcessContent"`
}

type TrackedEvents struct {
	LastTrackedEvent []LastTrackEvent `json:"LastTrackedEvent"`
}

var proxies []string
var proxy string
var requestData Request
var trackedEvents = make(map[string]LastTrackEvent)

func boolptr(b bool) *bool { return &b }

func strptr(s string) *string { return &s }

// redactTrackingNumber shows only last 4 characters for safe logging
func redactTrackingNumber(trackingID string) string {
	if len(trackingID) <= 4 {
		return strings.Repeat("*", len(trackingID))
	}
	return strings.Repeat("*", len(trackingID)-4) + trackingID[len(trackingID)-4:]
}

// validateProxyURL validates and normalizes proxy URLs, only allowing http/https
func validateProxyURL(proxyLine string) (string, error) {
	// Handle traditional IP:port format
	if strings.Contains(proxyLine, "://") {
		u, err := url.Parse(proxyLine)
		if err != nil {
			return "", fmt.Errorf("invalid proxy URL: %v", err)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return "", fmt.Errorf("unsupported proxy scheme: %s", u.Scheme)
		}
		return proxyLine, nil
	} else {
		// Traditional IP:port format - convert to http://
		parts := strings.Split(proxyLine, ":")
		if len(parts) != 2 {
			return "", fmt.Errorf("invalid proxy format, expected IP:port")
		}
		return fmt.Sprintf("http://%s:%s", parts[0], parts[1]), nil
	}
}

func readProxies() {
	lines, err := validation.ReadLinesSanitized("proxies.txt", nil)
	if err != nil {
		log.Printf("Error loading proxies: %v", err)
		return
	}

	for _, line := range lines {
		proxyURL, err := validateProxyURL(line)
		if err != nil {
			log.Printf("Skipping invalid proxy: %v", err)
			continue
		}

		// Parse URL to safely log only scheme and host
		if u, err := url.Parse(proxyURL); err == nil {
			log.Printf("Loaded proxy: %s://%s", u.Scheme, u.Host)
		}

		proxies = append(proxies, proxyURL)
	}

	if len(proxies) == 0 {
		log.Printf("Warning: No valid proxies loaded")
	} else {
		log.Printf("Loaded %d proxies", len(proxies))
	}
}

func rotateProxies() {
	if len(proxies) == 0 {
		log.Printf("Warning: No proxies available for rotation")
		proxy = ""
		return
	}

	var index = rand.Intn(len(proxies))
	proxy = proxies[index]

	// Log safely without exposing credentials
	if u, err := url.Parse(proxy); err == nil {
		log.Printf("Using proxy: %s://%s", u.Scheme, u.Host)
	}
}

func readTrackingNumbers() {
	numbers, err := validation.ReadLinesSanitized("tracking-numbers.txt", validation.ValidateTrackingNumber)
	if err != nil {
		log.Printf("Error loading tracking numbers: %v", err)
		return
	}

	requestData.NumberList = numbers

	if len(numbers) == 0 {
		log.Printf("Warning: No valid tracking numbers loaded")
	} else {
		log.Printf("Loaded %d tracking numbers", len(numbers))
		// Log redacted tracking numbers for verification
		for _, num := range numbers {
			log.Printf("Tracking number loaded: %s", redactTrackingNumber(num))
		}
	}
}

func installPlayright() {
	err := playwright.Install()

	if err != nil {
		log.Fatalf("Error installing Playright driver: %v", err)
	}
}

func fetchWithPlaywright(start int) {
	pw, err := playwright.Run()
	if err != nil {
		log.Printf("Error starting playwright: %v", err)
		return
	}
	defer pw.Stop()

	var browserOpts playwright.BrowserTypeLaunchOptions
	browserOpts.Headless = playwright.Bool(false)

	// Only set proxy if we have one
	if proxy != "" {
		browserOpts.Proxy = &playwright.Proxy{
			Server: proxy,
		}
	}

	browser, err := pw.Chromium.Launch(browserOpts)
	if err != nil {
		log.Printf("Error launching browser: %v", err)
		return
	}
	defer browser.Close()

	page, err := browser.NewPage()
	if err != nil {
		log.Printf("Error creating page: %v", err)
		return
	}

	page.SetViewportSize(1920, 1080)

	page.On("response", func(response playwright.Response) {
		url := response.URL()
		if strings.Contains(url, "services.yuntrack.com/Track/Query") {
			log.Printf("[+] Intercepted API response: %s", url)

			go func() {
				body, err := response.Body()
				if err != nil {
					log.Printf("Error reading response body: %v", err)
					return
				}

				var parsed Results
				if err := json.Unmarshal(body, &parsed); err != nil {
					log.Printf("Error parsing response JSON: %v", err)
					return
				}

				for _, result := range parsed.Results {
					_, ok := trackedEvents[result.TrackInfo.TrackingNumber]
					redactedID := redactTrackingNumber(result.TrackInfo.TrackingNumber)

					if !ok {
						if start == 0 {
							log.Printf("New tracking information found: %s", redactedID)
							sendHook(result.TrackInfo.LastTrackEvent, result.TrackInfo.TrackingNumber, false)
						}

						trackedEvents[result.TrackInfo.TrackingNumber] = result.TrackInfo.LastTrackEvent
					} else if trackedEvents[result.TrackInfo.TrackingNumber].ProcessContent != result.TrackInfo.LastTrackEvent.ProcessContent {
						log.Printf("Status for tracking %s information found!", redactedID)
						sendHook(result.TrackInfo.LastTrackEvent, result.TrackInfo.TrackingNumber, true)

						trackedEvents[result.TrackInfo.TrackingNumber] = result.TrackInfo.LastTrackEvent
					}
				}
			}()
		}
	})

	// Navigate with timeout and error handling
	_, err = page.Goto("https://www.yuntrack.com/",
		playwright.PageGotoOptions{
			WaitUntil: playwright.WaitUntilStateNetworkidle,
			Timeout:   playwright.Float(60000), // 60 seconds
		},
	)
	if err != nil {
		log.Printf("Error navigating to main page: %v", err)
		return
	}

	_, err = page.Goto(fmt.Sprintf("https://www.yuntrack.com/parcelTracking?id=%s",
		strings.Join(requestData.NumberList, ",")),
		playwright.PageGotoOptions{
			WaitUntil: playwright.WaitUntilStateNetworkidle,
			Timeout:   playwright.Float(90000), // 90 seconds
		},
	)
	if err != nil {
		log.Printf("Error navigating to tracking page: %v", err)
		return
	}

	log.Printf("Successfully completed tracking fetch for %d numbers", len(requestData.NumberList))
}

func sendHook(event LastTrackEvent, trackingID string, statusChanged bool) {
	username := "YunExpress Shipping"
	url := os.Getenv("DISCORD_WEBHOOK_URL")
	title := trackingID
	url_title := fmt.Sprintf("https://www.yuntrack.com/parcelTracking?id=%s", trackingID)

	var fields []discordwebhook.Field

	fields = append(fields, discordwebhook.Field{
		Name:   strptr("Content"),
		Value:  &event.ProcessContent,
		Inline: boolptr(false),
	})

	fields = append(fields, discordwebhook.Field{
		Name:   strptr("Location"),
		Value:  &event.ProcessLocation,
		Inline: boolptr(false),
	})

	fields = append(fields, discordwebhook.Field{
		Name:   strptr("Date"),
		Value:  &event.ProcessDate,
		Inline: boolptr(false),
	})

	if statusChanged {
		fields = append(fields, discordwebhook.Field{
			Name:   strptr("Type"),
			Value:  strptr("Status Change"),
			Inline: boolptr(false),
		})
	} else {
		fields = append(fields, discordwebhook.Field{
			Name:   strptr("Type"),
			Value:  strptr("New Tracking Number"),
			Inline: boolptr(false),
		})
	}

	message := discordwebhook.Message{
		Username: &username,
		Embeds: &[]discordwebhook.Embed{
			{
				Title:  &title,
				Url:    &url_title,
				Fields: &fields,

				Color: strptr("15648324"),
				Author: &discordwebhook.Author{
					Name: strptr("YunExpress Shipping"),
				},
			},
		},
	}

	err := discordwebhook.SendMessage(url, message)
	if err != nil {
		log.Println(err)
	}
}

func main() {
	var iterations = 1
	err := godotenv.Load(".env.local")
	if err != nil {
		log.Fatalf("Error loading .env.local: %v", err)
	}

	// Initialize random seed for proxy rotation
	rand.Seed(time.Now().UnixNano())

	installPlayright()
	readProxies()
	readTrackingNumbers()

	if len(requestData.NumberList) == 0 {
		log.Fatalf("No valid tracking numbers to process")
	}

	// Rate limiter: space out iterations to avoid overwhelming the service
	ticker := time.NewTicker(500 * time.Second)
	defer ticker.Stop()

	for iterations >= 0 {
		log.Printf("Starting tracking iteration %d", iterations)

		rotateProxies()
		fetchWithPlaywright(iterations)

		if iterations > 0 {
			log.Printf("Waiting for next iteration...")
			<-ticker.C
		}
		iterations = 1
	}
}
