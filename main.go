package main

import (
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/imroc/req/v3"
)

const baseURL = "https://books.toscrape.com/index.html"
const domainURL = "https://books.toscrape.com"
const individualBase = "https://books.toscrape.com/catalogue"

// Job struct holds info about job
type job struct {
	category, title, url string
}

type result struct {
	title        string
	url          string
	category     string
	imageURL     string
	price        string
	rating       float64
	isInStock    string
	stockAmount  string
	summary      string
	reviewsCount string
	upc          string
}

// worker creates a new worker
func worker(wg sync.WaitGroup, jobs chan job, termination chan bool) {

	defer wg.Done()

	// client to send request
	httpClient := newHttpClient()

	for {
		select {
		case unit, _ := <-jobs:
			//start working
			output := extractFromDetailsPage(httpClient, unit)
			fmt.Println(output)
		case <-termination:
			fmt.Println("Shutting down")
			return
		}
	}
}

// returns a new http client with some pre-confiuration
func newHttpClient() *req.Client {
	client := req.NewClient().SetTimeout(60 * time.Second).SetCommonRetryCount(5).SetCommonRetryHook(func(resp *req.Response, err error) {
		fmt.Println("Retrying after status: ", resp.StatusCode)
		fmt.Println(err)
	}).ImpersonateChrome().OnError(func(client *req.Client, req *req.Request, resp *req.Response, err error) {
		fmt.Println("Erorr has occurred:", err)
	})

	return client
}

func main() {

	// get a new client and a request
	client := newHttpClient()
	fmt.Println("Addr:", &client)
	req := client.R()

	log.Println("Trying to send get request: ", baseURL)
	homePageResp, err := req.Get(baseURL)

	if err != nil {
		log.Fatal(err)
	}

	log.Println("Status code: ", homePageResp.Status)
	homePage, err := goquery.NewDocumentFromReader(homePageResp.Body)

	if err != nil {
		log.Fatal(err)
	}

	type section struct {
		text, url string
	}

	categories := make([]*section, 0, 100)

	homePage.Find("ul.nav.nav-list ul li a").Each(func(i int, s *goquery.Selection) {
		text := strings.TrimSpace(s.Text())
		url := s.AttrOr("href", "")

		if len(text) > 0 && len(url) > 0 {
			categories = append(categories, &section{text: text, url: domainURL + "/" + url})
		}
	})

	if len(categories) > 0 {
		scrapeCategory(categories[0].text, categories[0].url)
	}

}

func scrapeCategory(text, url string) {
	// create a new http client for each function execution
	// will be useful in concurrent execution
	httpClient := newHttpClient()

	req := httpClient.R()
	log.Println("Trying to send get request:", url)
	pageResp, err := req.Get(url)
	if err != nil {
		log.Println(err)
		return
	}

	page, err := goquery.NewDocumentFromReader(pageResp.Body)

	if err != nil {
		log.Println(err)
		return
	}

	totalResults := page.Find("form.form-horizontal").First()
	re := regexp.MustCompile(`(?i)(\d+)\sresults`)
	match := re.FindStringSubmatch(totalResults.Text())
	total := ""

	if len(match) > 1 {
		total = match[1]
	}

	if total == "" {
		log.Println("No results found for category: ", text)
		return
	}

	totalCount, err := strconv.Atoi(total)
	if err != nil {
		log.Println(err)
		return
	}

	fmt.Println("Total count:", totalCount)

	re = regexp.MustCompile(`(?i)Page \d+ of (\d+)`)
	totalPages := strings.TrimSpace(page.Find("li.current").Text())

	count := 1
	currentPage := 1

	if totalPages == "" {
		fmt.Println("No further pages found for category:", text)
	} else {

		match = re.FindStringSubmatch(totalPages)
		if len(match) > 1 {
			temp := match[1]

			count, err = strconv.Atoi(temp)
			if err != nil {
				log.Println(err)
				return
			}

			fmt.Printf("Total pages for category: %s is %d\n", text, count)
		}
	}

	for currentPage <= count {

		log.Printf("Extracting items from page: %d for category: %s\n", currentPage, text)
		if currentPage > 1 {

			// make request for next page
		}

		books := extractFromListingPage(page)

		for key, val := range books {
			temp := job{category: text, title: key, url: val}
			fmt.Println(extractFromDetailsPage(httpClient, temp))
			os.Exit(1) // TODO: remove this later

		}
		currentPage++
	}
}

// extracts title and url from the listing page
func extractFromListingPage(doc *goquery.Document) map[string]string {

	// store title and url
	items := map[string]string{}

	doc.Find("article.product_pod > h3 > a").Each(func(i int, s *goquery.Selection) {
		title := s.AttrOr("title", "")
		link := s.AttrOr("href", "")

		if title != "" && link != "" {
			link = strings.ReplaceAll(link, "../../..", "")
			items[title] = individualBase + link
		}

	})

	return items
}

// extract all possible details from the details page
func extractFromDetailsPage(httpClient *req.Client, unit job) result {
	r := result{title: unit.title, category: unit.category, url: unit.url}
	log.Println("Trying to send get request: ", baseURL)
	pageResp, err := req.Get(baseURL)

	if err != nil {
		log.Fatal(err)
	}

	log.Println("Status code: ", pageResp.Status)
	page, err := goquery.NewDocumentFromReader(pageResp.Body)

	if err != nil {
		log.Fatal(err)
	}

	// image url
	r.imageURL = page.Find("div.item.active > img").AttrOr("src", "")

	// price
	r.price = strings.TrimSpace(page.Find("p.price_color").Text())

	// TODO: stock availabilit, units available, rating

	r.summary = strings.TrimSpace(page.Find("div#product_description + p").First().Text())

	return r
}
