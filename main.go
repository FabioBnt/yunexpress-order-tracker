package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/gtuk/discordwebhook"
	"github.com/joho/godotenv"
	"github.com/playwright-community/playwright-go"
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

func readProxies() {
	data, err := os.ReadFile("proxies.txt")
	if err != nil {
		log.Printf("Error loading proxies: %v", err)
	}

	lines := strings.SplitSeq(string(data), "\n")
	for line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Split(line, ":")
		if len(parts) != 2 {
			log.Printf("Skipping invalid proxy line: %s", line)
			continue
		}

		ip := parts[0]
		port := parts[1]

		proxyURL := fmt.Sprintf("http://%s:%s", ip, port)
		proxies = append(proxies, proxyURL)
	}
}

func rotateProxies() {
	var index = rand.Intn(len(proxies))

	proxy = proxies[index]
}

func readTrackingNumbers() {
	data, err := os.ReadFile("tracking-numbers.txt")
	if err != nil {
		log.Printf("Error loading tracking numbers: %v", err)
	}

	lines := strings.SplitSeq(string(data), "\n")

	for line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		requestData.NumberList = append(requestData.NumberList, line)
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
		log.Fatalf("Error starting playwright: %v", err)
	}

	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(false),
		Proxy: &playwright.Proxy{
			Server: proxy,
		},
	})
	if err != nil {
		log.Fatalf("Error launching browser: %v", err)
	}

	page, err := browser.NewPage()
	if err != nil {
		log.Fatalf("Error creating page: %v", err)
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

					if !ok {
						if start == 0 {
							log.Printf("New tracking information found: %v", result.TrackInfo.TrackingNumber)
							sendHook(result.TrackInfo.LastTrackEvent, result.TrackInfo.TrackingNumber, false)
						}

						trackedEvents[result.TrackInfo.TrackingNumber] = result.TrackInfo.LastTrackEvent
					} else if trackedEvents[result.TrackInfo.TrackingNumber].ProcessContent != result.TrackInfo.LastTrackEvent.ProcessContent {
						log.Printf("Status for tracking #%v information found!", result.TrackInfo.TrackingNumber)
						sendHook(result.TrackInfo.LastTrackEvent, result.TrackInfo.TrackingNumber, true)

						trackedEvents[result.TrackInfo.TrackingNumber] = result.TrackInfo.LastTrackEvent
					}
				}
			}()
		}
	})

	_, err = page.Goto("https://www.yuntrack.com/",
		playwright.PageGotoOptions{
			WaitUntil: playwright.WaitUntilStateNetworkidle,
		},
	)
	if err != nil {
		log.Fatalf("Error navigating to main page: %v", err)
	}

	_, err = page.Goto(fmt.Sprintf("https://www.yuntrack.com/parcelTracking?id=%s",
		strings.Join(requestData.NumberList, ",")),
		playwright.PageGotoOptions{
			WaitUntil: playwright.WaitUntilStateNetworkidle,
		},
	)
	if err != nil {
		log.Fatalf("Error navigating to tracking page: %v", err)
	}

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

	installPlayright()
	readProxies()
	readTrackingNumbers()

	for iterations >= 0 {
		rotateProxies()
		fetchWithPlaywright(iterations)
		time.Sleep(500 * time.Second)
		iterations = 1
	}
}
