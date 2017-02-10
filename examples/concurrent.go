package main

import (
	"fmt"
	"sync"
)

func f(n int, wg *sync.WaitGroup) {
	defer wg.Done()
	fmt.Printf("hello %d", n)
}

func main() {
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go f(i, &wg)
	}
	wg.Wait()
}
