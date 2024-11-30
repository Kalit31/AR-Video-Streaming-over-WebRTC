package server

import (
	"fmt"
	"net/http"

	"github.com/gorilla/websocket"
)

type Message struct {
    Type    string `json:"type"`
    Content string `json:"content"`
}

var upgrader = websocket.Upgrader{
    CheckOrigin: func(r *http.Request) bool {
        return true // Allow all connections
    },
}

var clients = make(map[*websocket.Conn]int) // Connected clients
var num_clients = 0

func Run() {
    http.HandleFunc("/ws", handleConnections)

    fmt.Println("Starting server on :8080")
    if err := http.ListenAndServe(":8080", nil); err != nil {
        panic("Failed to start server: " + err.Error())
    }
}

func handleConnections(w http.ResponseWriter, r *http.Request) {
    conn, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        fmt.Println("Error during connection upgrade:", err)
        return
    }
    defer conn.Close()

    clients[conn] = num_clients
    num_clients++

    // Listen for messages from the client
    for {
        var clientMsg Message
        err := conn.ReadJSON(&clientMsg)
        if err != nil {
            fmt.Println("Error reading message:", err)
            delete(clients, conn)
            break
        }

        fmt.Printf("Received message from client %d: %s\n", clients[conn], clientMsg.Type)

        // Broadcast message to all connected clients
        for client := range clients {
            if client != conn {
                fmt.Printf("Writing to client %d\n", clients[client])
                err := client.WriteJSON(clientMsg)
                if err != nil {
                    fmt.Println("Error writing message to client:", err)
                    client.Close()
                    delete(clients, client)
                }
            }
        }
    }
}