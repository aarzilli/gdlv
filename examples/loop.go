package main

import (
	"fmt"
)

const N = 23

func main() {
	r := make([]int, 23)
	a := []int{4, 8, 15, 16, 23, 42}
	for i := 0; i < 1000; i++ {
		n := a[i%len(a)]
		r[i%len(r)] += n
		fmt.Printf("%d\n", i)
	}

	for i := range r {
		fmt.Printf("%d ", r[i])
	}
	fmt.Printf("\n")
}
