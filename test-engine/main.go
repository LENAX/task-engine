package main

import (
	"fmt"
	"log"
	"os"
)

func main() {
	token := os.Getenv("QDHUB_TUSHARE_TOKEN")
	if token == "" {
		log.Fatal("QDHUB_TUSHARE_TOKEN is not set")
	}

	fmt.Println("token", token)
}
