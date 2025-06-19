package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/joho/godotenv"
	"github.com/temoto/robotstxt"
	"golang.org/x/net/html"
)

type Queue struct {
	queueLock *sync.RWMutex
	Url       []string
	Size      int64
}

type Crawler struct {
	TotalCrawlSize     int
	LastCrawled        time.Time
	TotalNumberOfQueue int
}

type CrawlContent struct {
	Url       string
	Topic     string
	Body      []byte
	CrawledAt time.Time
}

func NewQueue() *Queue {
	url := make([]string, 0)
	return &Queue{
		Url:       url,
		queueLock: new(sync.RWMutex),
	}
}

var urlVisited = sync.Map{}

func markVisitedUrl(url string) bool {
	sha := sha256.New()
	sha.Write([]byte(url))
	hashed := sha.Sum(nil)
	if _, loaded := urlVisited.LoadOrStore(hex.EncodeToString(hashed), true); loaded {
		return true
	}
	return false
}

func (q *Queue) enqueue(url string) {
	q.queueLock.Lock()
	q.Url = append(q.Url, url)
	q.queueLock.Unlock()
	atomic.AddInt64(&q.Size, 1)
}

func (q *Queue) dequeue() string {
	q.queueLock.Lock()
	if len(q.Url) == 0 {
		q.queueLock.Unlock()
		return ""
	}
	popped := q.Url[0]
	q.Url = q.Url[1:]
	q.queueLock.Unlock()
	atomic.AddInt64(&q.Size, -1)
	return popped
}

func (q *Queue) isEmpty() bool {
	q.queueLock.RLocker()
	defer q.queueLock.Unlock()
	return q.Size == 0
}

func main() {
	var url, dbCred string
	var wg, crawlGroup sync.WaitGroup

	if err := godotenv.Load(); err != nil {
		fmt.Fprintln(os.Stdout, "err: "+err.Error())
		fmt.Fprintln(os.Stdout, "continuing...")
	}

	url, dbCred = os.Getenv("CRAWLURL"), os.Getenv("DBCred")

	if url == "" {
		fmt.Fprintln(os.Stderr, "no url provided")
		os.Exit(1)
	}
	if dbCred == "" {
		fmt.Fprintln(os.Stdout, "no db cred provided, continuing...")
	}

	queue := NewQueue()
	body := make(chan []byte, 10)
	queue.enqueue(url) // starter url
	for i := 1; i <= 5; i++ {
		crawlGroup.Add(1)
		go crawlWebPage(queue, body, &crawlGroup)
	}

	crawlGroup.Wait()
	for i := 1; i <= 5; i++ {
		wg.Add(1)
		go extractTextDataFromHTML(body, &wg)
	}
	go func() {
		close(body)
	}()

	wg.Wait()
	fmt.Printf("Finished crawling data... from provided seed url: %s\n", url)
}

func crawlWebPage(queue *Queue, body chan []byte, wg *sync.WaitGroup) {
	defer wg.Done()
	for queue.Size > 0 {
		poppedUrl := queue.dequeue()
		purl, err := checkRobotstxt(poppedUrl)

		if err != nil {
			fmt.Printf("skipping -> %s: due to robots.txt guideline ->  %s", purl, err.Error())
			continue
		}

		if markVisitedUrl(purl) {
			fmt.Printf("skipping -> %s already visited", purl)
			continue
		}
		resp, err := sendreq(purl)
		if err != nil {
			fmt.Printf("%s: added url back to queue error: %s", purl, err.Error())
			queue.enqueue(purl)
			continue
		}
		if resp.StatusCode == 200 {
			bod, err := io.ReadAll(resp.Body)
			if err != nil {
				fmt.Printf("%s: added url back to queue error: %s", purl, err.Error())
				continue
			}
			fmt.Println("send data to extractTextfromHTML goroutine")
			body <- bod
		}
	}
}

func extractTextDataFromHTML(queueChan chan []byte, wg *sync.WaitGroup) {
	defer wg.Done()
	for bytes := range queueChan {
		reader := strings.NewReader(string(bytes))
		tokenizer := html.NewTokenizer(reader)

		for {
			tt := tokenizer.Next()
			if tt == html.ErrorToken {
				tokenErr := tokenizer.Err()
				if tokenErr == io.EOF {
					fmt.Printf("EOF: %s\n", tokenErr.Error())
					break
				}
				fmt.Printf("error tokenizing HTML: %v\n", tokenErr)
				break
			}

			if tt == html.StartTagToken || tt == html.EndTagToken {
				token := tokenizer.Token()

				switch token.Data {
				case "a":
					var href string
					for _, attr := range token.Attr {
						if attr.Key == "href" {
							href = strings.TrimSpace(attr.Val)
							if !strings.HasPrefix(href, "https") {
								continue
							}
							fmt.Printf("links: %s\n", href)
						}
					}
				}
			}
		}
	}
}

func checkRobotstxt(uri string) (string, error) {
	u, _ := url.Parse(uri)
	robotsUrl := u.Scheme + "://" + u.Host + "/robots.txt"
	resp, err := sendreq(robotsUrl)
	if err != nil {
		return "", err
	}
	rob, err := robotstxt.FromResponse(resp)
	if err != nil {
		return "", err
	}
	group := rob.FindGroup("*")
	if group.Test(uri) {
		return uri, nil
	} else {
		return "", errors.New("cannot crawl url...")
	}
}

func sendreq(url string) (*http.Response, error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; MyCrawler/1.0)")
	resp, err := client.Do(req)
	return resp, err
}
