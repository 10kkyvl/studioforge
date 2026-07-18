package main

import (
	"fmt"
	"os"
	"strings"
	"time"
)

func main() {
	joined := strings.Join(os.Args[1:], " ")
	if strings.Contains(joined, "--version") {
		fmt.Println("Rojo 7.99.0-fake")
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "serve" {
		fmt.Println("Rojo server listening")
		for {
			time.Sleep(time.Second)
		}
	}
	os.Exit(2)
}
