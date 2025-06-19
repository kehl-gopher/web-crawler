package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/joho/godotenv"
	"github.com/temoto/robotstxt"
)

type Queue struct {
	Url       []string
	Size      int64
	queueLock *sync.RWMutex
}

type Crawler struct {
	TotalCrawlSize int
	LastCrawled    time.Time
}

type CrawlContent struct {
	Url       string
	Topic     string
	Body      string
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

func (q *Queue) enqueue(value string) {
	q.queueLock.Lock()
	q.Url = append(q.Url, value)
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

func main() {
	var url, dbCred string
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

	// queryResp := make(chan []byte, 50)
	// urlChan := make(chan string, 10)
	// queue := NewQueue()

	// workers.... are crawlers.... and extractors both publish to queue>
	// consumers.... crawlers and extractors both consume.... from message in the queue
	//
	// both crawlers and extrators will read and write to the queue
	// using bfs search we search for urls current present in a page... and add to queue

	// 5 crawlers and 5
	fmt.Printf("Finished crawling data... from provided seed url: %s\n", url)
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
