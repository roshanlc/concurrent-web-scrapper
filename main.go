package main

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/imroc/req/v3"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
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

type Save struct {
	Title    string `bson:"title,omitempty"`
	Url      string `bson:"url,omitempty"`
	Category string `bson:"category,omitempty"`
	Price    string `bson:"price,omitempty"`
	Summary  string `bson:"summary,omitempty"`
}

func (r result) String() string {
	return fmt.Sprintf("Title: %s, Url: %s, Category: %s\nimageURL: %s, Price: %s, Rating: %v\nInStock: %s, stockAmount: %s\nSummary: %s\n",
		r.title, r.url, r.category, r.imageURL, r.price, r.rating, r.isInStock, r.stockAmount, r.summary)
}

// worker creates a new worker
func worker(wg *sync.WaitGroup, jobs chan job, results chan result) {

	defer wg.Done()

	// client to send request
	httpClient := newHttpClient()

	for work := range jobs {
		log.Println("Processing individual page: ", work.url)
		output := extractFromDetailsPage(httpClient, work)
		results <- output
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
	defer func() {
		if err := recover(); err != nil {
			log.Println("Suffered a panic.")
			fmt.Println(err)
		}
	}()

	// get a new client and a request
	client := newHttpClient()
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

	// fail early
	if len(categories) == 0 {
		log.Println("Zero catefories were extracted. Shutting down.")
		return
	}

	allJobs := make(chan job, 200) // buffered chanel
	allResults := make(chan result, 200)

	var wgResult sync.WaitGroup
	var wgCat sync.WaitGroup
	var wgJobs sync.WaitGroup

	for _, cat := range categories {
		wgCat.Add(1)
		go scrapeCategory(&wgCat, allJobs, cat.text, cat.url)
	}

	// only spawn 25 go-routines as workers
	for i := 0; i < 25; i++ {
		wgJobs.Add(1)
		go worker(&wgJobs, allJobs, allResults)
	}

	wgResult.Add(1)
	// go routine to save results
	go saveResult(&wgResult, allResults)

	wgCat.Wait()
	close(allJobs)
	wgJobs.Wait()
	close(allResults)
	// wait for all goroutine to end
	wgResult.Wait()

	log.Println("Scrapping completed...")
}

// save result coming from
func saveResult(wg *sync.WaitGroup, data chan result) {

	defer wg.Done()
	count := 0

	// fileName := "results.txt"
	// file, err := os.Open(fileName)

	// if err != nil {
	// 	log.Fatal("Unable to open file to save")
	// }

	// defer file.Close()

	// for val := range data {
	// 	fmt.Println("Writing result..")
	// 	file.WriteString(fmt.Sprintf("%s,%s,%s,%v,%s\n", val.title, val.category, val.url, val.price, val.price))
	// }

	clientOptions := options.Client().ApplyURI("mongodb://admin:password@localhost:27017/")

	client, err := mongo.Connect(context.TODO(), clientOptions)
	if err != nil {
		panic(err)
	}

	year, month, day := time.Now().Date()
	m := time.Now().Minute()
	collectionName := fmt.Sprintf("scrape-%d-%d-%d-%d", year, month, day, m)
	scrape := client.Database("scrape").Collection(collectionName)

	defer client.Disconnect(context.TODO())
	for val := range data {

		_, err = scrape.InsertOne(context.Background(), Save{Title: val.title, Url: val.url, Category: val.category, Price: val.price, Summary: val.summary})

		if err != nil {
			log.Println(err)
		} else {
			count++
			log.Println("Total rows added: ", count)
		}
	}

}

// scrape a category of books
func scrapeCategory(wg *sync.WaitGroup, jobs chan job, text, url string) {
	defer wg.Done()

	backupURL := url
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

	log.Println("Total count:", totalCount, " for category:", text)

	re = regexp.MustCompile(`(?i)Page \d+ of (\d+)`)
	totalPages := strings.TrimSpace(page.Find("li.current").Text())

	count := 1
	currentPage := 1

	if totalPages == "" {
		log.Println("No further pages found for category:", text)
	} else {

		match = re.FindStringSubmatch(totalPages)
		if len(match) > 1 {
			temp := match[1]

			count, err = strconv.Atoi(temp)
			if err != nil {
				log.Println(err)
				return
			}

			log.Printf("Total pages for category: %s is %d\n", text, count)
		}
	}

	for currentPage <= count {

		log.Printf("Extracting items from page: %d for category: %s\n", currentPage, text)
		if currentPage > 1 {

			// make request for next page
			temp := strings.ReplaceAll(backupURL, "index.html", "")
			url = fmt.Sprintf("%spage-%d.html", temp, currentPage)

			ok := validateURL(url)
			// validate the url first
			if !ok {
				currentPage++
				continue
			}
			log.Println("Trying to send get request:", url)

			pageResp, err := req.Get(url)
			if err != nil {
				log.Println(err)
				currentPage++
				continue
			}

			page, err = goquery.NewDocumentFromReader(pageResp.Body)

			if err != nil {
				log.Println(err)
				currentPage++
				continue
			}
		}

		books := extractFromListingPage(page)

		for key, val := range books {
			temp := job{category: text, title: key, url: val}
			jobs <- temp

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
	r.imageURL = page.Find("img").First().AttrOr("src", "")

	r.summary = strings.TrimSpace(page.Find("div#product_description + p").Text())

	// price
	r.price = strings.TrimSpace(page.Find("p.price_color").First().Text())

	// TODO: stock availabilit, units available, rating and others

	return r
}

func validateURL(link string) bool {
	_, err := url.ParseRequestURI(link)
	if err != nil {
		log.Println(err)
		return false
	}

	return true
}
