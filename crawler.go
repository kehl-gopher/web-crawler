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
	number    int64
	size      int64
}

type Crawler struct {
	PageCount          int
	TotalCrawlSize     int
	LastCrawledTime    time.Time
	SkippedPagesRobots int
}

type CrawlContent struct {
	Title     string    `bson:"title"`
	Url       string    `bson:"url"`
	Body      string    `bson:"body"`
	CrawledAt time.Time `bson:"crawled_at"`
}

func NewQueue() *Queue {
	url := make([]string, 0)
	return &Queue{
		Url:       url,
		queueLock: new(sync.RWMutex),
	}
}

var urlVisited = sync.Map{}

func visted(url string) {
	urlVisited.Store(hashurl(url), true)
}

func hashurl(url string) string {
	sha := sha256.New()
	sha.Write([]byte(url))
	hashed := sha.Sum(nil)
	return hex.EncodeToString(hashed)
}

func contain(url string) bool {
	if _, ok := urlVisited.Load(hashurl(url)); ok {
		return true
	}

	return false
}

func (q *Queue) enqueue(url string) {
	q.queueLock.Lock()
	q.Url = append(q.Url, url)
	q.queueLock.Unlock()
	atomic.AddInt64(&q.size, 1)
	atomic.AddInt64(&q.number, 1)
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
	atomic.AddInt64(&q.number, -1)
	return popped
}

func (q *Queue) isEmpty() bool {
	return q.number == 0
}

func (q *Queue) totalQueueSize() int64 {
	return q.number
}

func main() {
	var url, dbCred string
	var wg sync.WaitGroup

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
	urlChan := make(chan string, 30)

	queue.enqueue(url) // starter url

	wg.Add(3)

	tick := time.NewTicker(3 * time.Second)
	go func() {
		defer wg.Done()
		for {
			select {
			case url, ok := <-urlChan:
				if !ok {
					fmt.Println("channel close stopping enqueue go routine")
					return
				}

				fmt.Println(queue.number)
				if contain(url) {
					fmt.Println("skipping url already visited previously no need to addd to queue")
					continue
				}
				queue.enqueue(url)
			case <-tick.C:
				fmt.Println("Still waiting...")

			}
		}
	}()

	go crawlWebPage(queue, body, &wg)
	go extractTextDataFromHTML(body, &wg, urlChan)

	wg.Wait()
	fmt.Printf("Finished crawling data... from provided seed url: %s\n", url)
}

func newContent(title string, body string) {

}

func crawlWebPage(queue *Queue, body chan []byte, wg *sync.WaitGroup) {
	defer wg.Done()

	idle := 0
	for {

		if queue.isEmpty() {
			time.Sleep(500 * time.Millisecond)
			idle++
			if idle > 3 {
				break
			}
			continue
		}

		idle = 0

		poppedUrl := queue.dequeue()
		purl, err := checkRobotstxt(poppedUrl)
		
		if err != nil || purl == "" {
			fmt.Printf("skipping -> %s: due to robots.txt guideline ->  %s\n", purl, err.Error())
			continue
		}
		if contain(purl) {
			fmt.Printf("skipping -> %s already visited\n", purl)
			continue
		}
		resp, err := sendreq(purl)
		visted(purl)
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
			body <- bod
		}
	}

	defer close(body)
}

func extractTextDataFromHTML(queueChan chan []byte, wg *sync.WaitGroup, urlChan chan string) {
	defer wg.Done()
	var countLink int32
	var pageCount int32

	for bytes := range queueChan {
		reader := strings.NewReader(string(bytes))
		tokenizer := html.NewTokenizer(reader)

		for {
			tt := tokenizer.Next()
			if tt == html.ErrorToken {
				tokenErr := tokenizer.Err()
				if tokenErr == io.EOF {
					atomic.AddInt32(&pageCount, 1)
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
							if ok, err := restrictDomain(href); !ok || err != nil {
								fmt.Printf("skipping -> %s domain not confined with nexford.edu\n", href)
								continue
							}
							atomic.AddInt32(&countLink, 1)
							urlChan <- href
						}
					}
				}
			}
		}
	}

	defer close(urlChan)
}

func checkRobotstxt(uri string) (string, error) {
	u, _ := url.Parse(uri)
	if uri == "" {
		return "", errors.New("empty URI")
	}

	u, err := url.Parse(uri)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid URL: %s", uri)
	}
	robotsUrl := u.Scheme + "://" + u.Host + "/robots.txt"
	resp, err := sendreq(robotsUrl)
	if err != nil {
		return "", err
	}
	rob, err := robotstxt.FromResponse(resp)
	if err != nil {
		fmt.Println(err.Error(), rob)
		return "", err
	}
	group := rob.FindGroup("*")
	if group.Test(uri) {
		return uri, nil
	} else {
		return uri, errors.New("cannot crawl url...")
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

func restrictDomain(ur string) (bool, error) {
	u, err := url.Parse(ur)
	if err != nil {
		return false, err
	}
	host := strings.TrimPrefix(u.Host, "www.")
	return host == "nexford.edu", nil
}
