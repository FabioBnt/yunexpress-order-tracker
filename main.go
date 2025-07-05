package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

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

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
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

func getAuthToken() string {
	pw, err := playwright.Run()
	if err != nil {
		log.Fatalf("could not start playwright: %v", err)
	}
	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(false),
	})
	if err != nil {
		log.Fatalf("could not launch browser: %v", err)
	}
	page, err := browser.NewPage()
	if err != nil {
		log.Fatalf("could not create page: %v", err)
	}

	page.On("request", func(request playwright.Request) {
		if request.Method() == "POST" {
			headers, err := request.AllHeaders()
			if err != nil {
				fmt.Printf("Could not get headers: %v\n", err)
				return
			}

			fmt.Println("Request Headers:")
			for k, v := range headers {
				fmt.Printf("%s: %s\n", k, v)
			}

			postData, _ := request.PostData()
			fmt.Printf("Intercepted POST request to %s\n", request.URL())
			fmt.Printf("Body: %s\n", postData)
		}
	})

	time.Sleep(time.Second * 5)

	resp, err := page.Goto("https://www.yuntrack.com/parcelTracking?id=YT2517400706432402")

	time.Sleep(time.Second * 5)

	if err != nil {
		log.Fatalf("could not goto: %v", err)
	}

	requestData.Timestamp = time.Now().Unix()

	allH, err := resp.AllHeaders()

	fmt.Println(allH)

	if err != nil {
		log.Fatalf("could not get header: %v", err)
	}

	for name, value := range allH {
		if name == "Timestamp" {
			num, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				log.Fatalf("could not convert: %v", err)
			}
			requestData.Timestamp = num
			fmt.Print(num)
		}
	}

	time.Sleep(time.Second * 10)

	cookies, err := page.Context().Cookies()
	if err != nil {
		log.Fatalf("could not get cookies: %v", err)
	}

	for _, cookie := range cookies {
		if cookie.Name == "acw_tc" {
			fmt.Printf("Found cookie: %s = %s\n", cookie.Name, cookie.Value)
			if err = browser.Close(); err != nil {
				log.Fatalf("could not close browser: %v", err)
			}
			if err = pw.Stop(); err != nil {
				log.Fatalf("could not stop Playwright: %v", err)
			}
			return cookie.Value
		}
	}

	if err = browser.Close(); err != nil {
		log.Fatalf("could not close browser: %v", err)
	}
	if err = pw.Stop(); err != nil {
		log.Fatalf("could not stop Playwright: %v", err)
	}

	return "none"
}

func fetch(auth_token string) {
	proxyURL, err := url.Parse(proxy)
	if err != nil {
		log.Printf("Error parsing proxy URL: %v", err)
		return
	}

	transport := &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
	}

	client := &http.Client{
		Transport: transport,
	}

	requestData.CaptchaVerification = ""
	requestData.Year = 0
	requestData.Signature = auth_token

	jsonData, err := json.Marshal(requestData)
	if err != nil {
		log.Printf("Error marshaling request data: %v", err)
		return
	}

	req, err := http.NewRequest("POST", "https://services.yuntrack.com/Track/Query", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("Error creating request: %v", err)
		return
	}

	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://www.yuntrack.com")
	req.Header.Set("Referer", "https://www.yuntrack.com")
	//req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:140.0) Gecko/20100101 Firefox/140.0")

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error making POST request: %v", err)
		return
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Error reading response body: %v", err)
		return
	}

	log.Printf("%s", responseBody)

	var response Result
	err = json.Unmarshal(responseBody, &response)
	if err != nil {
		log.Printf("Error unmarshaling response body: %v", err)
		return
	}

	log.Printf("%v", response.TrackInfo.CustomerOrderNumber)
}

func main() {
	readProxies()
	rotateProxies()
	readTrackingNumbers()
	auth_token := getAuthToken()
	fmt.Print(auth_token)
	fetch(auth_token)
}
