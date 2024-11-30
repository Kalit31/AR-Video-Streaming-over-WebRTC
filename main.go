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
	generateStatsFlag := flag.Bool("generate_stats", false, "Generate statistics for client")

    // Parse the command-line flags
    flag.Parse()

	if *serverFlag {
		server.Run()
	} else if *clientFlag {
		client.Run(*generateStatsFlag)
	} else {
        fmt.Println("Please specify either --client or --server")
    }
}