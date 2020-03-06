[![Build Status](https://secure.travis-ci.org/JensRantil/go-sync.png?branch=master)](http://travis-ci.org/JensRantil/go-sync) [![Go Report Card](https://goreportcard.com/badge/github.com/JensRantil/go-sync)](https://goreportcard.com/report/github.com/JensRantil/go-sync) [![GoDoc](https://godoc.org/github.com/JensRantil/go-sync?status.svg)](https://godoc.org/github.com/JensRantil/go-sync)

`sync` with Context
===================
This is a small Golang library that tries to mimic (unless otherwise stated in
doc) the behaviour of

 * `sync.Mutex`
 * `sync.Cond`

but adds `context.Context` to support deadlines.

So far very little time has been spent on performance here, but here are some
crude benchmarks:

```
goos: darwin
goarch: amd64
pkg: github.com/JensRantil/sync-with-context
BenchmarkMutexLockUnlock-8              	25858257	        43.2 ns/op
BenchmarkStandardMutexLockUnlock-8      	122055702	         9.81 ns/op
BenchmarkMutexLockWithContextUnlock-8   	26493699	        43.7 ns/op
PASS
ok  	github.com/JensRantil/sync-with-context	4.665s
```
That is the `sync.Mutex` in standard library is about 77% faster than this mutex.
