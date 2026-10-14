// Package bbssec holds what the BBS's packages take from the top of the
// tree: the sample configuration, whose comments are the configuration
// editor's help.
package bbssec

import _ "embed"

// Sample is bbs.conf.sample.
//
//go:embed bbs.conf.sample
var Sample []byte
