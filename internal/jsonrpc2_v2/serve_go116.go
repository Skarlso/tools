// Copyright 2022 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build go1.16

package jsonrpc2

import (
	"net"
)

var errClosed = net.ErrClosed
