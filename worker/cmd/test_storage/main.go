package main

import (
	"fmt"
	"os"

	"github.com/pavandhadge/vectron/worker/internal/storage"
)

func main() {
	dbPath, err := os.MkdirTemp("", "pebbledb_main_test_")
	if err != nil {
		panic(fmt.Sprintf("failed to create temp dir: %v", err))
	}
	defer os.RemoveAll(dbPath)

	db := storage.NewPebbleDB()
	opts := &storage.Options{CreateIfMissing: true}
	err = db.Init(dbPath, opts)
	if err != nil {
		panic(fmt.Sprintf("failed to init db: %v", err))
	}
	defer db.Close()

	fmt.Println("Database initialized successfully.")

	key := []byte("hello from main")
	value := []byte("world in main")

	// Test Put
	fmt.Println("Putting value...")
	err = db.Put(key, value)
	if err != nil {
		fmt.Printf("Put failed: %v\n", err)
		return
	}

	// Test Get
	fmt.Println("Getting value...")
	retrievedValue, err := db.Get(key)
	if err != nil {
		fmt.Printf("Get failed: %v\n", err)
		return
	}
	fmt.Printf("Retrieved value: %s\n", retrievedValue)

	// Test Delete
	fmt.Println("Deleting value...")
	err = db.Delete(key)
	if err != nil {
		fmt.Printf("Delete failed: %v\n", err)
		return
	}
	fmt.Println("Value deleted.")

	// Test Get after Delete
	retrievedValue, err = db.Get(key)
	if err != nil {
		fmt.Printf("Get after delete failed: %v\n", err)
	}
	if retrievedValue == nil {
		fmt.Println("Successfully verified that value is deleted.")
	} else {
		fmt.Printf("Value was not deleted: %s\n", retrievedValue)
	}
}
