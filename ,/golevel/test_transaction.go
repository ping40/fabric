package main

// copy from https://github.com/syndtr/goleveldb/issues/129
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

	fmt.Println("Creating transaction...")
	tr, err := db.OpenTransaction()
	if err != nil {
		fmt.Println(err)
		return
	}

	syncChan := make(chan struct{})
	//	go anotherTxn(db, syncChan)
	go anotherWrite(db, syncChan)

	for i := 0; i < numRecords; i++ {
		if err := tr.Put(records[i], nil, nil); err != nil {
			fmt.Println(err)
			return
		}
		if i%10000 == 0 {
			printMemStats()
		}
	}

	pprof.Lookup("goroutine").WriteTo(os.Stdout, 1)

	if err := tr.Commit(); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("Transaction committed")
	printMemStats()
	writeHeapPprof("heap-1.pprof")

	time.Sleep(3 * time.Second)
	runtime.GC()

	printMemStats()
	writeHeapPprof("heap-2.pprof")
}

func anotherTxn(db *leveldb.DB, ch chan struct{}) {

	fmt.Println("in anotherTxn 01", time.Now())
	tx, err := db.OpenTransaction()
	if err != nil {
		fmt.Println(err)
		close(ch)
		return
	}
	fmt.Println("in anotherTxn 02", time.Now())
	time.Sleep(time.Second * 10)
	close(ch)
	fmt.Println("in anotherTxn 03", time.Now())
	tx.Discard()
}

func anotherWrite(db *leveldb.DB, ch chan struct{}) {

	fmt.Println("in anotherWrite 01", time.Now())
	batch := new(leveldb.Batch)
	batch.Put([]byte("foo"), []byte("value"))
	batch.Put([]byte("bar"), []byte("another value"))
	batch.Delete([]byte("baz"))
	//time.Sleep(time.Second * 1)
	fmt.Println("in anotherWrite 02, before write ", time.Now())

	err := db.Write(batch, nil)
	if err != nil {
		fmt.Println(err)
	}

	fmt.Println("in anotherWrite 03", time.Now())
	close(ch)

}
