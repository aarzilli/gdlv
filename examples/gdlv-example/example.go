package main

import (
	"fmt"
	"os"
)

type entry struct {
	value int
	name  string
}

var v = []entry{
	{0, "zero"},
	{1, "one"},
	{2, "two"},
	{3, "three"},
	{4, "four"},
	{5, "five"},
	{6, "six"},
}

func increase() {
	for i := 0; i <= len(v); i++ {
		v[i].value++
	}
}

func main() {
	fmt.Println(os.Args)
	otherFunc()
	increase()
}
