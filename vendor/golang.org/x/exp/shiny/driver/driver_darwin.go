// Copyright 2015 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build darwin && !metal
// +build darwin,!metal

package driver

import (
	"golang.org/x/exp/shiny/driver/gldriver"
	"golang.org/x/exp/shiny/screen"
)

func main(f func(screen.Screen)) {
	gldriver.Main(f)
}
