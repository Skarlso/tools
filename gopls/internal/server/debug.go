// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

// assert panics with the given msg if cond is not true.
func assert(cond bool, msg string) {
	if !cond {
		panic(msg)
	}
}
