package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"

	"cpu-scheduler-eval/internal/viewer"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8080", "HTTP listen address")
	runs := flag.String("runs", "runs", "directory containing versioned run JSON artifacts")
	flag.Parse()

	server := &http.Server{
		Addr:              *listen,
		Handler:           viewer.NewHandler(*runs),
		ReadHeaderTimeout: 5 * time.Second,
	}
	fmt.Printf("CPU scheduler trials: http://%s/ (runs: %s)\n", *listen, *runs)
	log.Fatal(server.ListenAndServe())
}
