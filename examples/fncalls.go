package main

import "fmt"

var count = 0

func afun(s string, n int) int {
	fmt.Printf("afun(%q, %d)\n", s, n)
	count++
	return count
}

func bfun(n int) string {
	return fmt.Sprintf("%d %d", n, b2fun())
}

func b2fun() int {
	count++
	return count
}

func cfun(a, b string) {
	fmt.Printf("cfun(%q, %q)\n", a, b)
}

func main() {
	cfun(bfun(afun(bfun(3), afun("first call", 0))), bfun(12))
}
