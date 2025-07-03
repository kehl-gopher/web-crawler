package main

import "sync"

type Queue struct {
	queueLock *sync.RWMutex
	url       string
	size      int
	totalSize int
}

const SeedUrl string = "https://nexford.edu/"

type Crawler struct {
}

func main() {

}
