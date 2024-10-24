package client

import (
	"fmt"
	"log"

	"github.com/gorilla/websocket"
)

func Run() {
    // Connect to the WebSocket server
    url := "ws://localhost:8080/ws"
    conn, _, err := websocket.DefaultDialer.Dial(url, nil)
    if err != nil {
        log.Fatal("Dial error:", err)
    }
    defer conn.Close()

    fmt.Println("Connected to the server")

    // Start a goroutine to listen for messages from the server
    go func(conn *websocket.Conn) {
        var inputMsg Message
        for {
            err := conn.ReadJSON(&inputMsg)
            if err != nil {
                log.Println("Read error:", err)
                return
            }
            fmt.Printf("Message from server: %s\n", inputMsg.Type)
            if (inputMsg.Type == "join"){
                go establishConnectionWithPeer(conn)
            } else if (inputMsg.Type == "offer"){
                go handleOffer(conn, inputMsg)
            } else if (inputMsg.Type == "answer"){
                go handleAnswer(inputMsg)
            } else if(inputMsg.Type == "iceCandidate"){
                go addICECandidate(inputMsg)
            }
        }
    }(conn)

    msg := Message{
        Type:    "join",
        Content: "true",
    }
    conn.WriteJSON(msg)


    <-connectionEstablishedChan
    fmt.Println("Successfully established a WebRTC connection between clients")

    openCameraFeed(userPeerConnection, userVideoTrack)

	select {}
}