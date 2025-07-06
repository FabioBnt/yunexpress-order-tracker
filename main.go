package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strings"

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

var proxies []string
var proxy string
var requestData Request

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

func fetchWithPlaywright() {
	pw, err := playwright.Run()
	if err != nil {
		log.Fatalf("Error starting playwright: %v", err)
	}

	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(true),
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

				log.Printf("Raw response body: %s", string(body))

				var parsed Result
				if err := json.Unmarshal(body, &parsed); err != nil {
					log.Printf("Error parsing response JSON: %v", err)
					return
				}

				log.Printf("Parsed tracking info: %+v", parsed)
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
		requestData.NumberList[0]),
		playwright.PageGotoOptions{
			WaitUntil: playwright.WaitUntilStateNetworkidle,
		},
	)
	if err != nil {
		log.Fatalf("Error navigating to tracking page: %v", err)
	}

	log.Printf("Tracking page loaded, waiting for API call to be intercepted...")

	if err := browser.Close(); err != nil {
		log.Fatalf("Error closing browser: %v", err)
	}

	if err := pw.Stop(); err != nil {
		log.Fatalf("Error stopping Playwright: %v", err)
	}
}

func main() {
	installPlayright()
	readProxies()
	readTrackingNumbers()

	for {
		rotateProxies()
		fetchWithPlaywright()
	}
}
