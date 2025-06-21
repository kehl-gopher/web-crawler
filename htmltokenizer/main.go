package main

import (
	"bytes"

	"golang.org/x/net/html"
)

func extractBodyText(htmlContent []byte) string {
	tokenizer := html.NewTokenizer(bytes.NewReader(htmlContent))

	inBody := false
	var result string

	for {
		tt := tokenizer.Next()
		switch tt {
		case html.ErrorToken:
			return result

		case html.StartTagToken, html.SelfClosingTagToken:
			t := tokenizer.Token()
			if t.Data == "body" {
				inBody = true
			}

		case html.EndTagToken:
			t := tokenizer.Token()
			if t.Data == "body" {
				inBody = false
			}

		case html.TextToken:
			if inBody {
				text := tokenizer.Token().Data
				result += text + " "
			}
		}
	}
}

// func main() {
// 	html := `<!DOCTYPE html><html><head><title>My Title</title></head><body><h1>Heading</h1><p>Paragraph</p><div>whoops</div></body></html>`
// 	text := extractBodyText([]byte(html))
// 	fmt.Println(text)
// }

// func main() {
// 	if len(os.Args) < 2 {
// 		fmt.Printf("usage:\n pagetitle <url>\n")
// 		os.Exit(1)
// 	}

// 	URL := os.Args[1]

// 	resp, err := http.Get(URL)

// 	if err != nil {
// 		log.Fatalf("error fetch URL: %v\n", err)
// 	}

// 	defer resp.Body.Close()
// 	if resp.StatusCode != http.StatusOK {
// 		log.Fatalf("response status code was %d\n", resp.StatusCode)
// 	}
// 	// check response content type
// 	ctype := resp.Header.Get("Content-Type")
// 	if !strings.HasPrefix(ctype, "text/html") {
// 		log.Fatalf("response content type was %s not text/html\n", ctype)
// 	}

// 	tokenizer := html.NewTokenizer(resp.Body)
// 	for {
// 		tokenType := tokenizer.Next()

// 		if tokenType == html.ErrorToken {
// 			err := tokenizer.Err()

// 			if err == io.EOF {
// 				break
// 			}
// 			log.Fatalf("error tokenizing HTML: %v", tokenizer.Err())
// 		}

// 		if tokenType == html.StartTagToken || tokenType == html.EndTagToken {
// 			// get the token
// 			token := tokenizer.Token()
// 			if "title" == token.Data {
// 				// the next token should be the page title
// 				tokenType = tokenizer.Next()

// 				if tokenType == html.TextToken {
// 					fmt.Println(tokenizer.Token().Data)
// 					// break
// 				}
// 			}
// 			if "a" == token.Data {
// 				tokenType = tokenizer.Next()
// 				linkText := strings.TrimSpace(tokenizer.Token().Data)
// 				if linkText != "" {
// 					fmt.Println("Link Text:", linkText)
// 				}
// 			}

// 			if "title" == token.Data {
// 				fmt.Println("----------->")
// 				tokenType = tokenizer.Next()
// 				if tokenType == html.TextToken {
// 					fmt.Println(tokenizer.Token().Data)
// 					// break
// 				}

// 			}
// 		}
// 	}
// }
