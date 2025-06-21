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

type Queue[T CrawlContent | CrawledURL | Crawler] struct {
	queueLock *sync.RWMutex
	Url       []T
	number    int64
	size      int64
}

type Crawler struct {
	PageCount          int
	TotalCrawlSize     int
	LastCrawledTime    time.Time
	TotalCrawledTime   time.Time
	SkippedPagesRobots int
}

type CrawlContent struct {
	Title   string    `bson:"title"`
	Body    string    `bson:"body"`
	Path    string    `bson:"path"`
	AddedAt time.Time `bson:"added_at"`
}

type CrawledURL struct {
	Url       string    `bson:"url"`
	Domain    string    `bson:"domain"`
	HTMLRaw   []byte    `bson:"html_raw"`
	CrawledAt time.Time `bson:"crawled_at"`
}

var (
	DBconnected        bool
	DBName             string = "crawledContent"
	CollectionContent  string = "content"
	CollectionMetaData string = "url_metadata"
	CrawlContentTopic  string = "add_content"
	MetadataTopic      string = "add_metadata"
	SeedUrl            string = "https://medium.com"
)

func NewQueue[T Crawler | CrawlContent | CrawledURL]() *Queue[T] {
	url := make([]T, 0)
	return &Queue[T]{
		Url:       url,
		queueLock: new(sync.RWMutex),
	}
}

var urlVisited = sync.Map{}

func visted(url string) {
	urlVisited.Store(hashurl(strings.TrimSuffix(url, "/")), true)
}

func hashurl(url string) string {
	sha := sha256.New()
	sha.Write([]byte(url))
	hashed := sha.Sum(nil)
	return hex.EncodeToString(hashed)
}

func contain(url string) bool {
	if _, ok := urlVisited.Load(hashurl(strings.TrimSuffix(url, "/"))); ok {
		return true
	}
	return false
}

func (q *Queue[T]) enqueue(url T) {
	q.queueLock.Lock()
	q.Url = append(q.Url, url)
	q.queueLock.Unlock()
	atomic.AddInt64(&q.size, 1)
	atomic.AddInt64(&q.number, 1)
}

func (q *Queue[T]) dequeue() *T {
	var zero T
	q.queueLock.Lock()
	if len(q.Url) == 0 {
		q.queueLock.Unlock()
		return &zero
	}
	popped := q.Url[0]
	q.Url = q.Url[1:]
	q.queueLock.Unlock()
	atomic.AddInt64(&q.number, -1)
	return &popped
}

func (q *Queue[T]) isEmpty() bool {
	q.queueLock.Lock()
	defer q.queueLock.Unlock()
	return q.number == 0
}

func (q *Queue[T]) Size() int64 {
	q.queueLock.Lock()
	defer q.queueLock.Unlock()
	return q.size
}

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

func AddToCollections[T CrawledURL | CrawlContent](ctx context.Context, client *mongo.Client, collName string, docs T) error {
	col := client.Database(DBName).Collection(collName)

	_, err := col.InsertOne(ctx, docs)
	if err != nil {
		fmt.Println("failed to insert data in mongo: " + err.Error())
		return err
	}
	fmt.Println("data inserted successfully")
	return nil
}

func main() {
	var dbCred string
	var mongoClient *mongo.Client

	if err := godotenv.Load(); err != nil {
		fmt.Println("err: "+err.Error(), "continuing...")
	}

	dbCred = os.Getenv("DBCred")

	if dbCred != "" {
		mongoClient = ConnectDB(dbCred)
	} else {
		fmt.Println("no db cred provided, continuing...")
	}

	//  work jhor... no go whyne me o...
	ps := newPubsub[any]()

	content := ps.subscribe(CrawlContentTopic, 100)

	ps.wg.Add(3)
	go func(content <-chan any) {
		defer ps.wg.Done()
		for con := range content {
			con, ok := con.(CrawlContent)
			fmt.Println(ok)
			if ok && mongoClient != nil {
				fmt.Println("insert to db")
				newContent[CrawlContent](mongoClient, CollectionContent, con)
			}
		}
	}(content)

	urlC := CrawledURL{Url: SeedUrl, Domain: ""}
	crawlerQueue := NewQueue[CrawledURL]()
	body := make(chan *CrawledURL, 10)

	crawlerQueue.enqueue(urlC)

	crawlerStat := &Crawler{PageCount: 0, TotalCrawlSize: 0, SkippedPagesRobots: 0}

	go func() {
		defer ps.wg.Done()
		crawlWebPage(crawlerQueue, body)
	}()

	go func() {
		defer ps.wg.Done()
		extractTextDataFromHTML(body, crawlerQueue, crawlerStat, ps)

	}()

	ps.wg.Wait()

	ps.Shutdown()
	fmt.Printf("Finished crawling data... from provided seed url: %s\n", SeedUrl)
}

func newContent[T CrawlContent | CrawledURL](client *mongo.Client, colName string, content T) {
	if DBconnected {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if client != nil {
			err := AddToCollections[T](ctx, client, colName, content)
			if err != nil {
				fmt.Println("inserting data error: " + err.Error())
			} else {
				fmt.Println("inserting data complete: ")
			}
		}
	}
}

func crawlWebPage(queue *Queue[CrawledURL], body chan *CrawledURL) {

	idle := 0
	for {

		if queue.isEmpty() {
			time.Sleep(500 * time.Millisecond)
			idle++
			if idle > 3 {
				close(body)
				break
			}
			continue
		}
		idle = 0
		poppedUrl := queue.dequeue()
		purl, err := checkRobotstxt(poppedUrl.Url)

		if err != nil || purl == "" {
			fmt.Printf("skipping -> %s: due to robots.txt guideline ->  %s\n", purl, err.Error())
			continue
		}

		if contain(purl) {
			fmt.Printf("queue can no longer queue anymore %d\n", queue.Size())
			continue
		}
		resp, err := sendreq(purl)
		visted(purl)
		if err != nil {
			fmt.Printf("%s: added url back to queue error: %s\n", purl, err.Error())
			queue.enqueue(*poppedUrl)
			continue
		}
		if resp.StatusCode == 200 {
			bod, err := io.ReadAll(resp.Body)
			if err != nil {
				fmt.Printf("%s: added url back to queue error: %s\n", purl, err.Error())
				continue
			}
			poppedUrl.HTMLRaw = bod
			// fmt.Println(string(bod))V
			body <- poppedUrl
		}
	}

}

func extractTextDataFromHTML(queueChan chan *CrawledURL, queue *Queue[CrawledURL], crawler *Crawler, ps *PubSub[any]) {
	var skipTags = map[string]bool{
		"script":   true,
		"style":    true,
		"noscript": true,
		"template": true,
		"iframe":   true,
		"canvas":   true,
		"svg":      true,
		"meta":     true,
		"link":     true,
		"head":     true,
		"object":   true,
		"embed":    true,
		"nav":      true,
		"footer":   true,
		"form":     true,
		"img":      true,
	}

	// var pageCount int32
	var (
		inBody  bool
		inTitle bool
		title   string
		words   []string
	)
	for urlq := range queueChan {
		reader := strings.NewReader(string(urlq.HTMLRaw))
		tokenizer := html.NewTokenizer(reader)

		for {
			tt := tokenizer.Next()
			if tt == html.ErrorToken {
				tokenErr := tokenizer.Err()
				if tokenErr == io.EOF {
					body := strings.Join(words, " ")
					path := path(urlq.Url)
					cc := CrawlContent{Title: title, Body: body, AddedAt: time.Now(), Path: path}
					ps.publish(CrawlContentTopic, cc)
					fmt.Printf("EOF: %s\n", tokenErr.Error())
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
					href := getHref(token)
					if href == "" || contain(href) {
						fmt.Println("skipping...", "url", href, queue.Size())
						continue
					}
					newUrl := CrawledURL{
						Url: href,
					}
					fmt.Println(newUrl)
					queue.enqueue(newUrl)
					fmt.Printf("[%s] link added to queue queue size is %d\n", href, queue.Size())
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
		if restriceted, err := restrictDomain(attr.Val); err == nil && restriceted {
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

	return baseURL.Hostname() == linkURL.Hostname()
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

func path(ul string) string {
	u, err := url.Parse(ul)
	if err != nil {
		return ""
	}
	return u.Path
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

	su, err := url.Parse(SeedUrl)
	if err != nil {
		return false, err
	}

	return u.Host == su.Host, nil
}
