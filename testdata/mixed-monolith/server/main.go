package main

import (
	"log"
	"net/http"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/orders/{id}/cancel", cancelHandler)
	log.Fatal(http.ListenAndServe(":8080", mux))
}
