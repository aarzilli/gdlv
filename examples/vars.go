package main

import (
	"fmt"
	"runtime"
	"time"
)

type Int int

type Point struct {
	X, Y int
}

type Struct struct {
	S string
}

type Struct2 struct {
	field1   int
	field2   int
	fieldacc int
}

func main() {
	longstr := "very long string 0123456789a0123456789b0123456789c0123456789d0123456789e0123456789f0123456789g012345678h90123456789i0123456789j0123456789 0123456789a0123456789b0123456789c0123456789d0123456789e0123456789f0123456789g012345678h90123456789i0123456789j0123456789 0123456789a0123456789b0123456789c0123456789d0123456789e0123456789f0123456789g012345678h90123456789i0123456789j0123456789 0123456789a0123456789b0123456789c0123456789d0123456789e0123456789f0123456789g012345678h90123456789i0123456789j0123456789 0123456789a0123456789b0123456789c0123456789d0123456789e0123456789f0123456789g012345678h90123456789i0123456789j0123456789"
	longbytearr := []byte(longstr)
	longrunearr := []rune(longstr)
	longintarr := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40}
	var x int = 34
	var y Int = 5
	var nx int = -4
	var ux uint = 18
	var f float64 = 30000000000000000
	var pointarr = []Point{{1, 1}, {2, 3}, {4, 4}, {1, 5}}
	var p = Point{1, 2}
	var pp = &Point{3, 4}
	var iface interface{} = &Point{1, 2}
	var iface2 interface{} = &Struct{S: longstr}
	t0 := time.Now()
	struct2 := Struct2{}

	runtime.Breakpoint()

	fmt.Println(longstr, longbytearr, longrunearr, x, y, nx, ux, longintarr, f, pointarr, p, iface, iface2, t0, pp, struct2)

	for i := 0; i < 3; i++ {
		a := []int{1, 2, 3}
		for i := 0; i < 3; i++ {
			a := []int{a[0] + 1, a[0] + 2, a[0] + 3}
			for i := 0; i < 3; i++ {
				fmt.Printf("%d %v\n", i, a)
			}
		}
	}

	pointarr[0].X = 10
	pointarr = append(pointarr, Point{1, 6})
}
