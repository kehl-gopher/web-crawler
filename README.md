# 🕷️ Go Web Crawler

This is a simple web crawler I built using **Go**, an HTML tokenizer, and **MongoDB** for data storage. It uses a **Breadth-First Search (BFS)** strategy to crawl pages, extracts useful information, and stores it in a structured format.

---

## 📌 What It Does

* Starts crawling from a seed URL.
* Uses BFS to explore and queue links.
* Parses each page using Go’s HTML tokenizer.
* Extracts information such as:

  * Page titles
  * Meta descriptions
  * All anchor tag links (`<a href>`)
* Saves the extracted data to MongoDB.

---

## 🧠 How It Works

### 1. **Crawling with BFS**

I implemented a BFS queue to make sure pages are visited layer by layer:

* Each new URL goes into a queue.
* Before enqueuing, I check if the URL has already been visited using a thread-safe map.
* As I dequeue and process each page, I extract and queue new links found on it.

### 2. **HTML Parsing**

Instead of using heavy HTML parsers, I used Go’s `html.NewTokenizer()`:

* It allows me to tokenize and scan the HTML stream efficiently.
* I focus on tokens like `<title>`, `<meta name="description">`, and `<a href>`.
* This low-level parsing gives me full control over what and how I extract data.

### 3. **Storing Data in MongoDB**

After parsing a page, I store the results in MongoDB in a structured format like this:

```json
{
  "url": "https://example.com",
  "title": "Example Title",
  "description": "This is an example description.",
  "links": [
    "https://example.com/about",
    "https://example.com/contact"
  ]
}
```

This makes it easy for me to query or analyze the data later.

---

## ⚙️ Tech Stack

* **Language**: Go
* **HTML Parsing**: `golang.org/x/net/html`
* **Database**: MongoDB
* **Concurrency**: Channels, goroutines, and mutexes

---

## 🚀 How to Run

1. **Install dependencies:**

   ```bash
   go mod tidy
   ```

2. **Set up MongoDB connection** (either via a `.env` file or directly in your config).

3. **Run the crawler:**

   ```bash
   go run main.go
   ```

---

## 📈 Features I Might Add Later

* `robots.txt` support
* Rate limiting to avoid spamming sites
* Domain restriction (stay within the same host)
* Custom depth limit
* Export results as CSV or JSON

---

## 🙋🏽‍♂️ Why I Built This

I built this project to sharpen my skills in:

* Writing efficient and concurrent Go code
* Parsing HTML without relying on large third-party libraries
* Structuring and storing scraped data
* Building scalable and real-world backend components
