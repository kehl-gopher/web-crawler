package main

import (
	"context"
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
	"unicode"

	"github.com/joho/godotenv"
	"github.com/temoto/robotstxt"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
	"golang.org/x/net/html"
)

var (
	urlVisited         = sync.Map{}
	DBconnected        bool
	DBName             string = "crawledContent"
	CollectionContent  string = "content"
	CollectionMetaData string = "url_metadata"
)

const SeedUrl string = "https://nexford.edu/"

type Queue struct {
	queueLock *sync.Mutex
	url       []string
	size      int
	totalSize int
}

type Crawler struct {
	startTime          time.Time
	pageCount          int32
	skippedPagesRobots int32
	crawlerSize        int32
}

func newQueue() *Queue {
	url := make([]string, 0)
	return &Queue{
		url:       url,
		queueLock: new(sync.Mutex),
	}
}

// add to queue
func (q *Queue) enqueue(data string) {
	q.queueLock.Lock()
	q.url = append(q.url, data)
	q.size++
	q.totalSize++
	q.queueLock.Unlock()
}

// pop element from queue
func (q *Queue) dequeue() string {
	q.queueLock.Lock()
	url := q.url[0]
	q.url = q.url[1:]
	q.size--
	q.queueLock.Unlock()
	return url
}

// check if queue is empty
func (q *Queue) isEmpty() bool {
	q.queueLock.Lock()
	defer q.queueLock.Unlock()
	return q.size == 0
}

func (cs *Crawler) size() int32 {
	return atomic.LoadInt32(&cs.crawlerSize)
}

func (q *Queue) qsize() int {
	q.queueLock.Lock()
	defer q.queueLock.Unlock()
	return q.size
}

func (q *Queue) qtotalSize() int {
	q.queueLock.Lock()
	defer q.queueLock.Unlock()
	return q.totalSize
}

// connect to mongoDB database
func ConnectDB(connStr string) *mongo.Client {
	client, err := mongo.Connect(options.Client().ApplyURI(connStr))
	if err != nil {
		DBconnected = false
		fmt.Println("could not connect to mongdb: " + err.Error())
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = client.Ping(ctx, readpref.Primary())
	if err != nil {
		DBconnected = false
		fmt.Println("could not ping: " + err.Error())
		return nil
	}
	func(client *mongo.Client) {
		collection := client.Database(DBName).Collection(CollectionContent)
		textModelIndex := mongo.IndexModel{
			Keys: bson.D{
				{Key: "title", Value: "text"},
				{Key: "body", Value: "text"},
			},
		}
		_, err := collection.Indexes().CreateOne(ctx, textModelIndex)
		if err != nil {
			fmt.Println("unable to create text index on collection: " + err.Error())
		}
		collection = client.Database(DBName).Collection(CollectionMetaData)

		uniqueModelIndex := mongo.IndexModel{
			Keys: bson.D{
				{Key: "path", Value: 1},
			},
			Options: options.Index().SetUnique(true),
		}
		_, err = collection.Indexes().CreateOne(ctx, uniqueModelIndex)
		if err != nil {
			fmt.Println("unable to create unique index on url: " + err.Error())
		}
	}(client)

	func(client *mongo.Client) {
		collection := client.Database(DBName).Collection(CollectionMetaData)
		uniqueModelIndex := mongo.IndexModel{
			Keys: bson.D{
				{Key: "url", Value: 1},
			},
			Options: options.Index().SetUnique(true),
		}
		_, err := collection.Indexes().CreateOne(ctx, uniqueModelIndex)
		if err != nil {
			fmt.Println("unable to create unique index on url: " + err.Error())
		}
	}(client)

	DBconnected = true
	fmt.Println("database connected successfully")
	return client
}

// create mongo collection
func AddToCollections(ctx context.Context, client *mongo.Client, collName string, docs interface{}) error {
	col := client.Database(DBName).Collection(collName)

	_, err := col.InsertOne(ctx, docs)
	if err != nil {
		fmt.Println("failed to insert data in mongo: " + err.Error())
		return err
	}
	return nil
}

func (cs *Crawler) markvisted(url string) {
	urlVisited.Store(hashurl(strings.TrimSuffix(url, "/")), true)
	atomic.AddInt32(&cs.crawlerSize, 1)
}

func hashurl(url string) string {
	sha := sha256.New()
	sha.Write([]byte(url))
	hashed := sha.Sum(nil)
	return hex.EncodeToString(hashed)
}

func (cs *Crawler) visited(url string) bool {
	if _, ok := urlVisited.Load(hashurl(strings.TrimSuffix(url, "/"))); ok {
		return true
	}
	return false
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
		return uri, errors.New("cannot crawl url")
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

func tokenize(text string) []string {
	var body strings.Builder
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsPunct(r) {
			body.WriteRune(r)
		} else {
			body.WriteByte(' ')
		}
	}
	return strings.Fields(body.String())
}

func getHref(tt html.Token) string {
	for _, attr := range tt.Attr {
		if attr.Key != "href" {
			continue
		}
		if isSameDomain(SeedUrl, attr.Val) {
			return attr.Val
		}
	}
	return ""
}

func isSameDomain(base, link string) bool {
	baseURL, err1 := url.Parse(base)
	linkURL, err2 := url.Parse(link)

	if err1 != nil || err2 != nil {
		return false
	}

	host := strings.TrimPrefix(linkURL.Hostname(), "www.")
	return baseURL.Hostname() == host
}

func main() {
	var (
		dbCred      string
		mongoClient *mongo.Client
		contentByte = make(chan []byte, 10)
		wg          sync.WaitGroup
	)
	if err := godotenv.Load(".env"); err != nil {
		fmt.Println("no environment file set skipping...")
	} else {
		dbCred = os.Getenv("DBcred")
	}

	if dbCred != "" {
		mongoClient = ConnectDB(dbCred)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	cs := &Crawler{startTime: time.Now(), pageCount: 0, crawlerSize: 0, skippedPagesRobots: 0}
	que := newQueue()
	que.enqueue(SeedUrl)
	cs.markvisted(SeedUrl)

	go sendRequest(que.dequeue(), contentByte)

	content := <-contentByte

	extractContent(que, content, cs, mongoClient)

	// send request every 1 second
	tick := time.NewTicker(1 * time.Second)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				url := que.dequeue()
				sendRequest(url, contentByte)
			}
		}
	}()

	// read and extract content from contentByte
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case content, ok := <-contentByte:
				if !ok {
					return
				}
				if len(content) == 0 {
					continue
				}
				extractContent(que, content, cs, mongoClient)
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(5 * time.Second)
		idle := 1
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if que.isEmpty() && len(contentByte) == 0 {
					idle++
					if idle > 3 {
						close(contentByte)
						cancel()
					}
				} else {
					idle = 0
				}
			}
		}
	}()

	wg.Wait()
	duration := time.Since(cs.startTime).Minutes()
	fmt.Printf("-------------------------------> finished crawling data... from provided seed url: %s <--------------------------------- \n", SeedUrl)
	fmt.Println("queue size after crawl", que.qsize()) //for debugging
	fmt.Printf("Pages Crawled: %d\n", cs.pageCount)
	fmt.Printf("Crawl Minute per Page: %.4fs\n", duration/float64(cs.pageCount))
	fmt.Printf("Crawl Success Ratio: %.2f\n", float64(cs.pageCount)/float64(cs.crawlerSize))
	fmt.Printf("queue size %d\n", que.qtotalSize())
	fmt.Printf("crawl size %d\n", cs.crawlerSize)
	fmt.Printf("skipped page robots.txt: %d\n", cs.skippedPagesRobots)

}

// send request to url
func sendRequest(url string, contentByte chan []byte) {
	url, err := checkRobotstxt(url)
	if err != nil {
		fmt.Printf("%s -> %s violated robots.txt", err.Error(), url)
		contentByte <- []byte{}
		return
	}
	if resp, err := sendreq(url); err != nil {
		fmt.Println("unexpected error while sending request: ", err.Error())
		contentByte <- []byte{}
		return
	} else {
		fmt.Println("sent request to -> ", url)
		byte, err := io.ReadAll(resp.Body)
		if err != nil {
			fmt.Println(err.Error())
			return
		}
		contentByte <- byte
	}
}

// extract content from html byte body
func extractContent(que *Queue, contentByte []byte, cs *Crawler, mg *mongo.Client) {
	var skipTags = map[string]bool{
		"script":     true,
		"style":      true,
		"noscript":   true,
		"template":   true,
		"iframe":     true,
		"canvas":     true,
		"svg":        true,
		"meta":       true,
		"link":       true,
		"head":       true,
		"object":     true,
		"embed":      true,
		"javascript": true,
		"nav":        true,
		"footer":     true,
		"form":       true,
		"span":       true,
		"img":        true,
	}

	parseHTML(que, contentByte, cs, skipTags)
}

func parseHTML(que *Queue, content []byte, cs *Crawler, skipTags map[string]bool) {
	contentReader := strings.NewReader(string(content))
	tokenizer := html.NewTokenizer(contentReader)
	for {
		var (
			inBody  bool
			inTitle bool
			title   string
			words   []string
		)
		tt := tokenizer.Next()
		if tt == html.ErrorToken {
			tokenErr := tokenizer.Err()
			if tokenErr == io.EOF {
				atomic.AddInt32(&cs.pageCount, 1)
				break
			}

			break
		}
		switch tt {
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokenizer.Token()
			if _, ok := skipTags[token.Data]; ok {
				tt = tokenizer.Next()
				continue
			}
			if token.Data == "title" {
				inTitle = true
			}
			if token.Data == "body" {
				inBody = true
			}
			if token.Data == "a" {
				tokenizer.Next()
				href := strings.TrimSpace(getHref(token))
				if !cs.visited(href) && href != "" {
					fmt.Printf("added %s to queue: queue size is %d\n", href, que.qsize())
					cs.markvisted(href)
					que.enqueue(href)
				}
			}
		case html.EndTagToken:
			token := tokenizer.Token()
			if token.Data == "title" {
				inTitle = false
			}
			if token.Data == "body" {
				inBody = false
			}
		case html.TextToken:
			if inTitle && title == "" {
				title = strings.TrimSpace(tokenizer.Token().Data)
			}
			if inBody && len(words) < 1000 {
				text := strings.TrimSpace(tokenizer.Token().Data)
				tokens := tokenize(text)
				remaining := 1000 - len(tokens)
				if len(tokens) > remaining {
					words = append(words, tokens[:remaining]...)
				} else {
					words = append(words, tokens...)
				}
			}
		}
	}
}
