package main

import (
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
	"time"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
)

func printMemStats() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Printf("sys: %d, alloc: %d, idle: %d\n", m.HeapSys,
		m.HeapAlloc, m.HeapIdle)
}

func writeHeapPprof(name string) {
	w, err := os.Create(name)
	if err != nil {
		fmt.Println(err)
		return
	}
	pprof.WriteHeapProfile(w)
	w.Close()
}

func main() {
	opts := opt.Options{
		Strict: opt.DefaultStrict,
	}
	db, err := leveldb.OpenFile("testldbbatch.db", &opts)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer db.Close()

	// ~295MB of data.
	fmt.Println("Creating records...")
	const numRecords = 200000
	const recSize = 1550
	data := make([]byte, numRecords*recSize)
	records := make([][]byte, numRecords)
	var offset int
	for i := 0; i < numRecords; i++ {
		records[i] = data[offset : offset+recSize : offset+recSize]
		offset += recSize
	}
	fmt.Println("Records created")

	// Print mem stats: ~325M
	printMemStats()
	writeHeapPprof("heap-0.pprof")

	// Add records to batch.
	fmt.Println("Creating batch...")
	batch := new(leveldb.Batch)
	batch.Load(make([]byte, 12, 12+numRecords*recSize+numRecords*11))
	for i := 0; i < numRecords; i++ {
		batch.Put(records[i], nil)
	}
	fmt.Println("Batch created")

	// Print mem stats: ~680MB
	printMemStats()
	writeHeapPprof("heap-1.pprof")

	fmt.Println("Writing batch...")
	if err := db.Write(batch, nil); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("Batch written")

	// Print mem stats: ~1.8GB!
	printMemStats()
	writeHeapPprof("heap-2.pprof")

	time.Sleep(3 * time.Second)
	runtime.GC()

	printMemStats()
	writeHeapPprof("heap-3.pprof")
}
