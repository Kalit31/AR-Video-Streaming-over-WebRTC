package main

import (
	"flag"
	"fmt"
	"websocket_tests/client"
	server "websocket_tests/signalling_server"
)

func main(){
	// Define flags
    clientFlag := flag.Bool("client", false, "Run as client")
    serverFlag := flag.Bool("server", false, "Run as server")

    // Parse the command-line flags
    flag.Parse()

	if *serverFlag {
		server.Run()
	} else if *clientFlag {
		client.Run()
	} else {
        fmt.Println("Please specify either --client or --server")
    }
}