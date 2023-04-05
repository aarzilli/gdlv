// Copyright 2015 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build (linux && !android) || dragonfly || openbsd
// +build linux,!android dragonfly openbsd

package driver

import (
	"golang.org/x/exp/shiny/driver/x11driver"
	"golang.org/x/exp/shiny/screen"
)

func main(f func(screen.Screen)) {
	x11driver.Main(f)
}
