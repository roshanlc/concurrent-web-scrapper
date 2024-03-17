package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/imroc/req/v3"
)

const baseURL = "https://books.toscrape.com/index.html"
const domainURL = "books.toscrape.com"

func main() {

	client := req.NewClient().SetTimeout(60 * time.Second)
	client.ImpersonateChrome()

	req := client.R().SetRetryCount(5).SetRetryHook(func(resp *req.Response, err error) {
		fmt.Println("Retrying after status: ", resp.StatusCode)
		fmt.Println(err)
	})

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

	sections := []*section{}

	homePage.Find("ul.nav.nav-list ul li a").Each(func(i int, s *goquery.Selection) {
		text := strings.TrimSpace(s.Text())
		url := s.AttrOr("href", "")

		sections = append(sections, &section{text: text, url: url})
	})

	for _, val := range sections {
		fmt.Printf("Text: %s, url: %s\n", val.text, val.url)
	}

}
