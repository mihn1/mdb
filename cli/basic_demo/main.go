package main

import (
	"log"

	"github.com/mihn1/mdb/pkg/db"
)

func main() {
	db, err := db.Open("/tmp/mdbtest", &db.Options{
		MemTableSize: 1024 * 1024, // 1MB
	})

	if err != nil {
		log.Fatal("Failed to open DB:", err)
	}

	err = db.Put([]byte("key1"), []byte("value1"))
	if err != nil {
		log.Fatal("Failed to put key1:", err)
	}

	value, err := db.Get([]byte("key1"))
	if err != nil {
		log.Fatal("Failed to get key1:", err)
	}

	log.Println("Got key1:", string(value))
}
